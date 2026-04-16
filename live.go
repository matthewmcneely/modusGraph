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
	"sync"
	"time"

	"github.com/dgraph-io/dgo/v250/protos/api"
	"github.com/dgraph-io/dgraph/v25/chunker"
	"github.com/dgraph-io/dgraph/v25/filestore"
	"github.com/dgraph-io/dgraph/v25/protos/pb"
	"github.com/dgraph-io/dgraph/v25/x"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

const (
	maxRoutines       = 4
	batchSize         = 1000
	numBatchesInBuf   = 100
	progressFrequency = 5 * time.Second
)

// mutator abstracts mutation submission for the LiveLoader.
type mutator interface {
	mutate(ctx context.Context, mu *api.Mutation) (map[string]string, error)
}

// namespaceMutator implements mutator for the embedded engine path.
type namespaceMutator struct {
	ns *Namespace
}

func (m *namespaceMutator) mutate(ctx context.Context, mu *api.Mutation) (map[string]string, error) {
	_, err := m.ns.Mutate(ctx, []*api.Mutation{mu})
	return nil, err
}

// uidAllocator abstracts UID allocation for the LiveLoader.
// The embedded Engine satisfies this interface directly.
type uidAllocator interface {
	LeaseUIDs(n uint64) (*pb.AssignedIds, error)
}

type liveLoader struct {
	mut        mutator
	uidAlloc   uidAllocator // nil for gRPC (server allocates)
	blankNodes map[string]string
	mutex      sync.RWMutex
	logger     logr.Logger
}

func (n *Namespace) Load(ctx context.Context, schemaPath, dataPath string) error {
	schemaData, err := os.ReadFile(schemaPath)
	if err != nil {
		return fmt.Errorf("error reading schema file [%v]: %w", schemaPath, err)
	}
	if err := n.AlterSchema(ctx, string(schemaData)); err != nil {
		return fmt.Errorf("error altering schema: %w", err)
	}

	if err := n.LoadData(ctx, dataPath); err != nil {
		return fmt.Errorf("error loading data: %w", err)
	}
	return nil
}

// TODO: Add support for CSV file
func (n *Namespace) LoadData(inCtx context.Context, dataDir string) error {
	ll := &liveLoader{
		mut:        &namespaceMutator{ns: n},
		uidAlloc:   n.engine,
		blankNodes: make(map[string]string),
		logger:     n.engine.logger,
	}
	return loadData(inCtx, ll, dataDir)
}

// loadData runs the core data-loading pipeline: find files, spawn file
// processors, and feed mutations through nqch to the consumer goroutine.
func loadData(inCtx context.Context, ll *liveLoader, dataDir string) error {
	fs := filestore.NewFileStore(dataDir)
	files := fs.FindDataFiles(dataDir, []string{".rdf", ".rdf.gz", ".json", ".json.gz"})
	if len(files) == 0 {
		return errors.Errorf("no data files found in [%v]", dataDir)
	}
	ll.logger.Info("Found data files to process", "count", len(files))

	// Here, we build a context tree so that we can wait for the goroutines towards the
	// end. This also ensures that we can cancel the context tree if there is an error.
	rootG, rootCtx := errgroup.WithContext(inCtx)
	procG, procCtx := errgroup.WithContext(rootCtx)
	procG.SetLimit(maxRoutines)

	// start a goroutine to do the mutations
	start := time.Now()
	nqudsProcessed := 0
	nqch := make(chan *api.Mutation, 10000)
	rootG.Go(func() error {
		ticker := time.NewTicker(progressFrequency)
		defer ticker.Stop()

		last := nqudsProcessed
		for {
			select {
			case <-rootCtx.Done():
				return rootCtx.Err()

			case <-ticker.C:
				elapsed := time.Since(start).Round(time.Second)
				rate := float64(nqudsProcessed-last) / progressFrequency.Seconds()
				ll.logger.Info("Data loading progress", "elapsed", x.FixedDuration(elapsed),
					"nquadsProcessed", nqudsProcessed,
					"writesPerSecond", fmt.Sprintf("%5.0f", rate))
				last = nqudsProcessed

			case nqs, ok := <-nqch:
				if !ok {
					return nil
				}
				uidMap, err := ll.mut.mutate(rootCtx, nqs)
				if err != nil {
					return fmt.Errorf("error applying mutations: %w", err)
				}
				// Merge returned blank-node -> UID mappings (gRPC path).
				// For the embedded path uidMap is nil, so this is a no-op.
				if len(uidMap) > 0 {
					ll.mutex.Lock()
					for k, v := range uidMap {
						ll.blankNodes[k] = v
					}
					ll.mutex.Unlock()
				}
				nqudsProcessed += len(nqs.Set)
			}
		}
	})

	for _, datafile := range files {
		procG.Go(func() error {
			return ll.processFile(procCtx, fs, datafile, nqch)
		})
	}

	// Wait until all the files are processed
	if errProcG := procG.Wait(); errProcG != nil {
		rootG.Go(func() error {
			return errProcG
		})
	}

	// close the channel and wait for the mutations to finish
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
		if isJson, err := chunker.IsJSONData(rd); err == nil {
			if isJson {
				loadType = chunker.JsonFormat
			} else {
				return errors.Errorf("unable to figure out data format for [%v]", filename)
			}
		}
	}

	g, ctx := errgroup.WithContext(inCtx)
	ck := chunker.NewChunker(loadType, batchSize)
	nqbuf := ck.NQuads()

	g.Go(func() error {
		buffer := make([]*api.NQuad, 0, numBatchesInBuf*batchSize)

		drain := func() {
			for len(buffer) > 0 {
				sz := batchSize
				if len(buffer) < batchSize {
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
						return fmt.Errorf("error getting UID for subject: %w", err)
					}
					if len(nq.ObjectId) > 0 {
						nq.ObjectId, err = l.uid(nq.Namespace, nq.ObjectId)
						if err != nil {
							return fmt.Errorf("error getting UID for object: %w", err)
						}
					}
				}

				buffer = append(buffer, nqs...)
				if len(buffer) < numBatchesInBuf*batchSize {
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
				return fmt.Errorf("error chunking data: %w", errChunk)
			}
			if err := ck.Parse(chunkBuf); err != nil {
				return fmt.Errorf("error parsing chunk: %w", err)
			}
			// We do this here in case of io.EOF, so that we can flush the last batch.
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

	// gRPC mode: server allocates UIDs, so return the blank node name as-is.
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
		return "", fmt.Errorf("error allocating UID: %w", err)
	}

	uid = fmt.Sprintf("%#x", asUID.StartId)
	l.blankNodes[key] = uid
	return uid, nil
}
