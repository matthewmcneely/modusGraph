/*
 * SPDX-FileCopyrightText: © 2017-2026 Istari Digital, Inc.
 * SPDX-License-Identifier: Apache-2.0
 */

package modusgraph

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dgraph-io/dgo/v250/protos/api"
	"github.com/dgraph-io/dgraph/v25/chunker"
	"github.com/dgraph-io/dgraph/v25/filestore"
	"github.com/dgraph-io/dgraph/v25/protos/pb"
	"github.com/dgraph-io/dgraph/v25/x"
	"github.com/go-logr/logr"
	"github.com/matthewmcneely/modusgraph/load"
	"golang.org/x/sync/errgroup"
)

const (
	defaultMaxRoutines       = 4
	defaultNumBatchesInBuf   = 100
	defaultNqchBufferSize    = 10000
	defaultProgressFrequency = 5 * time.Second
)

// Mutator abstracts mutation submission for the LiveLoader.
// Implementations exist for both the embedded engine and gRPC clients.
type Mutator interface {
	// Mutate submits a batch of NQuads. The returned map contains blank node
	// to UID mappings from the server (gRPC path); for the embedded engine
	// the map is nil because blank nodes are pre-resolved locally.
	Mutate(ctx context.Context, mu *api.Mutation) (map[string]string, error)
}

// namespaceMutator implements Mutator for the embedded engine path.
type namespaceMutator struct {
	ns *Namespace
}

func (m *namespaceMutator) Mutate(ctx context.Context, mu *api.Mutation) (map[string]string, error) {
	_, err := m.ns.Mutate(ctx, []*api.Mutation{mu})
	return nil, err
}

// UIDAllocator allocates UIDs for blank node resolution.
// For the embedded engine, the Engine type satisfies this directly.
// For gRPC, a bulk-allocating implementation leases UIDs from the Zero leader.
// A nil UIDAllocator means blank nodes are sent to the server as-is.
type UIDAllocator interface {
	LeaseUIDs(n uint64) (*pb.AssignedIds, error)
}

type liveLoader struct {
	mut        Mutator
	uidAlloc   UIDAllocator // nil when server allocates UIDs
	blankNodes map[string]string
	mutex      sync.RWMutex
	logger     logr.Logger
	batchSize  int
}

// Load reads a schema file and data directory, applying both to this namespace.
func (n *Namespace) Load(ctx context.Context, schemaPath, dataPath string) error {
	schemaData, err := os.ReadFile(schemaPath)
	if err != nil {
		return fmt.Errorf("read schema file [%v]: %w", schemaPath, err)
	}
	if err := n.AlterSchema(ctx, string(schemaData)); err != nil {
		return fmt.Errorf("alter schema: %w", err)
	}
	if err := n.LoadData(ctx, dataPath); err != nil {
		return fmt.Errorf("load data: %w", err)
	}
	return nil
}

// LoadData loads RDF or JSON data files from dataDir into this namespace.
func (n *Namespace) LoadData(inCtx context.Context, dataDir string) error {
	ll := &liveLoader{
		mut:        &namespaceMutator{ns: n},
		uidAlloc:   n.engine,
		blankNodes: make(map[string]string),
		logger:     n.engine.logger,
	}
	return loadData(inCtx, ll, dataDir, load.Options{})
}

// loadData runs the core data-loading pipeline: find files, spawn file
// processors, and feed mutations to concurrent workers.
func loadData(inCtx context.Context, ll *liveLoader, dataDir string, opts load.Options) error {
	fs := filestore.NewFileStore(dataDir)

	var allFiles []string
	if err := filepath.Walk(dataDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			allFiles = append(allFiles, path)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("walk data dir [%s]: %w", dataDir, err)
	}

	files := opts.FilterFiles(allFiles)
	if opts.SortFiles != nil {
		files = opts.SortFiles(files)
	}
	if len(files) == 0 {
		return fmt.Errorf("no data files found in [%v]", dataDir)
	}

	batchSize := opts.GetBatchSize()
	numWorkers := opts.GetMutationWorkers()
	ll.batchSize = batchSize
	ll.logger.Info("Found data files to process", "count", len(files))

	rootG, rootCtx := errgroup.WithContext(inCtx)
	procG, procCtx := errgroup.WithContext(rootCtx)
	procG.SetLimit(defaultMaxRoutines)

	start := time.Now()
	var nquadsProcessed atomic.Int64
	nqch := make(chan *api.Mutation, defaultNqchBufferSize)

	// Progress reporter — runs outside the errgroup so it doesn't block
	// completion. Stopped via context cancellation when loadData returns.
	tickCtx, tickCancel := context.WithCancel(rootCtx)
	defer tickCancel()
	go func() {
		ticker := time.NewTicker(defaultProgressFrequency)
		defer ticker.Stop()

		var last int64
		for {
			select {
			case <-tickCtx.Done():
				return
			case <-ticker.C:
				cur := nquadsProcessed.Load()
				elapsed := time.Since(start).Round(time.Second)
				rate := float64(cur-last) / defaultProgressFrequency.Seconds()
				ll.logger.Info("Data loading progress", "elapsed", x.FixedDuration(elapsed),
					"nquadsProcessed", cur,
					"writesPerSecond", fmt.Sprintf("%5.0f", rate))
				last = cur
			}
		}
	}()

	// Mutation workers — with pre-allocated UIDs, mutations are independent
	// and can execute concurrently.
	for range numWorkers {
		rootG.Go(func() error {
			for nqs := range nqch {
				if _, err := ll.mut.Mutate(rootCtx, nqs); err != nil {
					return fmt.Errorf("apply mutations: %w", err)
				}
				nquadsProcessed.Add(int64(len(nqs.Set)))
			}
			return nil
		})
	}

	for _, datafile := range files {
		procG.Go(func() error {
			return ll.processFile(procCtx, fs, datafile, nqch)
		})
	}

	if err := procG.Wait(); err != nil {
		rootG.Go(func() error { return err })
	}

	close(nqch)
	return rootG.Wait()
}

func (l *liveLoader) processFile(inCtx context.Context, fs filestore.FileStore,
	filename string, nqch chan *api.Mutation) error {

	l.logger.Info("Processing data file", "filename", filename)

	rd, cleanup := fs.ChunkReader(filename, nil)
	defer cleanup()

	loadType := chunker.DataFormat(filename, "")
	if loadType == chunker.UnknownFormat {
		isJSON, err := chunker.IsJSONData(rd)
		if err == nil && isJSON {
			loadType = chunker.JsonFormat
		} else {
			return fmt.Errorf("unable to determine data format for [%v]", filename)
		}
	}

	bs := l.batchSize
	g, ctx := errgroup.WithContext(inCtx)
	ck := chunker.NewChunker(loadType, bs)
	nqbuf := ck.NQuads()

	g.Go(func() error {
		buffer := make([]*api.NQuad, 0, defaultNumBatchesInBuf*bs)

		drain := func() {
			for len(buffer) > 0 {
				sz := bs
				if len(buffer) < bs {
					sz = len(buffer)
				}
				nqch <- &api.Mutation{Set: buffer[:sz]}
				buffer = buffer[sz:]
			}
		}

		loop := true
		for loop {
			select {
			case <-ctx.Done():
				return ctx.Err()

			case nqs, ok := <-nqbuf.Ch():
				if !ok {
					loop = false
					break
				}
				if len(nqs) == 0 {
					continue
				}

				var err error
				for _, nq := range nqs {
					nq.Subject, err = l.uid(nq.Namespace, nq.Subject)
					if err != nil {
						return fmt.Errorf("get UID for subject: %w", err)
					}
					if len(nq.ObjectId) > 0 {
						nq.ObjectId, err = l.uid(nq.Namespace, nq.ObjectId)
						if err != nil {
							return fmt.Errorf("get UID for object: %w", err)
						}
					}
				}

				buffer = append(buffer, nqs...)
				if len(buffer) < defaultNumBatchesInBuf*bs {
					continue
				}
				drain()
			}
		}
		drain()
		return nil
	})

	g.Go(func() error {
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			chunkBuf, errChunk := ck.Chunk(rd)
			if errChunk != nil && errChunk != io.EOF {
				return fmt.Errorf("chunk data: %w", errChunk)
			}
			if err := ck.Parse(chunkBuf); err != nil {
				return fmt.Errorf("parse chunk: %w", err)
			}
			if errChunk != nil {
				break
			}
		}

		nqbuf.Flush()
		return nil
	})

	return g.Wait()
}

func (l *liveLoader) uid(ns uint64, val string) (string, error) {
	key := x.NamespaceAttr(ns, val)

	l.mutex.RLock()
	uid, ok := l.blankNodes[key]
	l.mutex.RUnlock()
	if ok {
		return uid, nil
	}

	if l.uidAlloc == nil {
		return val, nil
	}

	l.mutex.Lock()
	defer l.mutex.Unlock()

	uid, ok = l.blankNodes[key]
	if ok {
		return uid, nil
	}

	asUID, err := l.uidAlloc.LeaseUIDs(1)
	if err != nil {
		return "", fmt.Errorf("allocate UID: %w", err)
	}

	uid = fmt.Sprintf("%#x", asUID.StartId)
	l.blankNodes[key] = uid
	return uid, nil
}
