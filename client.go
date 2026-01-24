/*
 * SPDX-FileCopyrightText: © Hypermode Inc. <hello@hypermode.com>
 * SPDX-License-Identifier: Apache-2.0
 */

package modusgraph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"

	"github.com/dgraph-io/dgo/v250"
	"github.com/dgraph-io/dgo/v250/protos/api"
	dg "github.com/dolan-in/dgman/v2"
	"github.com/go-logr/logr"
	"github.com/go-playground/validator/v10"
)

// Client provides an interface for ModusGraph operations
type Client interface {
	// Insert adds a new object or slice of objects to the database.
	// The object must be a pointer to a struct with appropriate dgraph tags.
	Insert(context.Context, any) error

	// InsertRaw adds a new object or slice of objects to the database.
	// The object must be a pointer to a struct with appropriate dgraph tags.
	// This is a no-op for remote Dgraph clients. For local clients, this
	// function mutates the Dgraph engine directly. No unique checks are performed.
	// The `UID` field for any objects must be set using the Dgraph blank node
	// prefix concept (e.g. "_:user1") to allow the engine to generate a UID for the object.
	InsertRaw(context.Context, any) error

	// Upsert inserts an object if it doesn't exist or updates it if it does.
	// This operation requires a field with a unique directive in the dgraph tag.
	// If no predicates are specified, the first predicate with the `upsert` tag will be used.
	// If none are specified in the predicates argument, the first predicate with the `upsert` tag
	// will be used.
	Upsert(context.Context, any, ...string) error

	// Update modifies an existing object in the database.
	// The object must be a pointer to a struct and must have a UID field set.
	Update(context.Context, any) error

	// Get retrieves a single object by its UID and populates the provided object.
	// The object parameter must be a pointer to a struct.
	Get(context.Context, any, string) error

	// Query creates a new query builder for retrieving data from the database.
	// Returns a *dg.Query that can be further refined with filters, pagination, etc.
	Query(context.Context, any) *dg.Query

	// Delete removes objects with the specified UIDs from the database.
	Delete(context.Context, []string) error

	// Close releases all resources used by the client.
	// It should be called when the client is no longer needed.
	Close()

	// UpdateSchema ensures the database schema matches the provided object types.
	// Pass one or more objects that will be used as templates for the schema.
	UpdateSchema(context.Context, ...any) error

	// GetSchema retrieves the current schema definition from the database.
	// Returns a string containing the full schema in Dgraph Schema Definition Language.
	GetSchema(context.Context) (string, error)

	// DropAll removes the schema and all data from the database.
	DropAll(context.Context) error

	// DropData removes all data from the database but keeps the schema intact.
	DropData(context.Context) error

	// QueryRaw executes a raw Dgraph query with optional query variables.
	// The `query` parameter is the Dgraph query string.
	// The `vars` parameter is a map of variable names to their values, used to parameterize the query.
	QueryRaw(context.Context, string, map[string]string) ([]byte, error)

	// DgraphClient returns a gRPC Dgraph client from the connection pool and a cleanup function.
	// The cleanup function must be called when finished with the client to return it to the pool.
	DgraphClient() (*dgo.Dgraph, func(), error)
}

const (
	// dgraphURIPrefix is the prefix for Dgraph server connections
	dgraphURIPrefix = "dgraph://"

	// fileURIPrefix is the prefix for file-based local connections
	fileURIPrefix = "file://"
)

var (
	clientMap     = make(map[string]Client)
	clientMapLock sync.RWMutex
)

// StructValidator is the interface for struct validation.
// This is compatible with github.com/go-playground/validator/v10.Validate.
type StructValidator interface {
	// StructCtx validates a struct with context support.
	StructCtx(ctx context.Context, s interface{}) error
}

// clientOptions holds configuration options for the client.
//
// autoSchema: whether to automatically manage the schema.
// poolSize: the size of the dgo client connection pool.
// maxEdgeTraversal: the maximum number of edges to traverse when querying.
// namespace: the namespace for the client.
// logger: the logger for the client.
// validator: the validator instance for struct validation.
type clientOptions struct {
	autoSchema       bool
	poolSize         int
	maxEdgeTraversal int
	cacheSizeMB      int
	namespace        string
	logger           logr.Logger
	validator        StructValidator
}

// ClientOpt is a function that configures a client
type ClientOpt func(*clientOptions)

// WithAutoSchema enables automatic schema management
func WithAutoSchema(enable bool) ClientOpt {
	return func(o *clientOptions) {
		o.autoSchema = enable
	}
}

// WithPoolSize sets the size of the dgraph client connection pool
func WithPoolSize(size int) ClientOpt {
	return func(o *clientOptions) {
		o.poolSize = size
	}
}

// WithNamespace sets the namespace for the client
func WithNamespace(namespace string) ClientOpt {
	return func(o *clientOptions) {
		o.namespace = namespace
	}
}

// WithLogger sets a structured logger for the client
func WithLogger(logger logr.Logger) ClientOpt {
	return func(o *clientOptions) {
		o.logger = logger
	}
}

// WithMaxEdgeTraversal sets the maximum depth of edges to traverse when fetching an object
func WithMaxEdgeTraversal(max int) ClientOpt {
	return func(o *clientOptions) {
		o.maxEdgeTraversal = max
	}
}

// WithCacheSizeMB sets the memory cache size in MB (only applicable for embedded databases).
// A good starting point for a system with a moderate amount of RAM (e.g., 8-16GB) would be
// between 256 MB and 1 GB. Dgraph itself often defaults to a 1GB cache. In order to minimize
// resource usage sans configuration, the default is set to 64 MB.
func WithCacheSizeMB(size int) ClientOpt {
	return func(o *clientOptions) {
		o.cacheSizeMB = size
	}
}

// WithValidator sets a validator instance for struct validation.
// The validator will be used to validate structs before insert, upsert, and update operations.
// If no validator is provided, validation will be skipped.
// Any type implementing StructValidator can be used, including *validator.Validate from
// github.com/go-playground/validator/v10.
func WithValidator(v StructValidator) ClientOpt {
	return func(o *clientOptions) {
		o.validator = v
	}
}

// NewValidator creates a new validator instance with default settings.
// This is a convenience function for creating a validator to use with WithValidator.
// It returns a *validator.Validate from github.com/go-playground/validator/v10.
func NewValidator() *validator.Validate {
	return validator.New()
}

// NewClient creates a new graph database client instance based on the provided URI.
//
// The function supports two URI schemes:
//   - dgraph://host:port - Connects to a remote Dgraph instance
//   - file:///path/to/db - Creates or opens a local file-based database
//
// Optional configuration can be provided via the opts parameter:
//   - WithAutoSchema(bool) - Enable/disable automatic schema creation for inserted objects
//   - WithPoolSize(int) - Set the connection pool size for better performance under load
//   - WithMaxEdgeTraversal(int) - Set the maximum number of edges to traverse when fetching an object
//   - WithNamespace(string) - Set the database namespace for multi-tenant installations
//   - WithLogger(logr.Logger) - Configure structured logging with custom verbosity levels
//   - WithCacheSizeMB(int) - Set the memory cache size in MB (only applicable for embedded databases)
//   - WithValidator(*validator.Validate) - Set a validator instance for struct validation before mutations
//
// The returned Client provides a consistent interface regardless of whether you're
// connected to a remote Dgraph cluster or a local embedded database. This abstraction
// helps prevent the connection issues that can occur with raw gRPC/bufconn setups.
//
// For file-based URIs, the client maintains a singleton Engine instance to ensure
// data consistency across multiple client connections to the same database.
func NewClient(uri string, opts ...ClientOpt) (Client, error) {
	// Default options
	options := clientOptions{
		autoSchema:       false,
		poolSize:         10,
		namespace:        "",
		maxEdgeTraversal: 10,
		cacheSizeMB:      64,             // 64 MB
		logger:           logr.Discard(), // No-op logger by default
	}

	// Apply provided options
	for _, opt := range opts {
		opt(&options)
	}

	// TODO: implement namespace support for v25
	if options.namespace != "" {
		options.logger.Info("Warning, namespace is set, but it is not supported in this version")
	}

	client := client{
		uri:     uri,
		options: options,
		logger:  options.logger,
	}

	clientMapLock.Lock()
	defer clientMapLock.Unlock()
	key := client.key()
	if _, ok := clientMap[key]; ok {
		return clientMap[key], nil
	}

	switch {
	case strings.HasPrefix(uri, dgraphURIPrefix):
		client.pool = newClientPool(options.poolSize, func() (*dgo.Dgraph, error) {
			client.logger.V(2).Info("Opening new Dgraph connection", "uri", uri)
			return dgo.Open(uri)
		}, client.logger)
		dg.SetLogger(client.logger)
		clientMap[key] = client
		return client, nil
	case strings.HasPrefix(uri, fileURIPrefix):
		// parse off the file:// prefix
		uri = uri[len(fileURIPrefix):]
		if _, err := os.Stat(uri); err != nil {
			return nil, err
		}
		engine, err := NewEngine(Config{
			dataDir:     uri,
			logger:      client.logger,
			cacheSizeMB: options.cacheSizeMB,
		})
		if err != nil {
			return nil, err
		}
		client.engine = engine
		client.pool = newClientPool(options.poolSize, func() (*dgo.Dgraph, error) {
			client.logger.V(2).Info("Getting Dgraph client from engine", "location", uri)
			return engine.GetClient()
		}, client.logger)
		dg.SetLogger(client.logger)
		clientMap[key] = client
		return client, nil
	}
	return nil, errors.New("invalid uri")

}

type client struct {
	uri     string
	engine  *Engine
	options clientOptions
	pool    *clientPool
	logger  logr.Logger
}

func (c client) isLocal() bool {
	return strings.HasPrefix(c.uri, fileURIPrefix)
}

func (c client) key() string {
	return fmt.Sprintf("%s:%t:%d:%d:%d:%s", c.uri, c.options.autoSchema, c.options.poolSize,
		c.options.maxEdgeTraversal, c.options.cacheSizeMB, c.options.namespace)
}

func checkPointer(obj any) error {
	if reflect.TypeOf(obj).Kind() != reflect.Ptr {
		return errors.New("object must be a pointer")
	}
	return nil
}

// validateStruct validates a struct using the configured validator
func (c client) validateStruct(ctx context.Context, obj any) error {
	if c.options.validator == nil {
		return nil // No validator configured, skip validation
	}

	// Handle both single structs and slices
	val := reflect.ValueOf(obj)
	if val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return fmt.Errorf("cannot validate nil pointer")
		}
		val = val.Elem()
	}

	if val.Kind() == reflect.Slice {
		for i := 0; i < val.Len(); i++ {
			elem := val.Index(i)
			if elem.Kind() == reflect.Ptr {
				if elem.IsNil() {
					return fmt.Errorf("cannot validate nil pointer at index %d", i)
				}
				elem = elem.Elem()
			}
			if err := c.options.validator.StructCtx(ctx, elem.Interface()); err != nil {
				return err
			}
		}
	} else {
		return c.options.validator.StructCtx(ctx, obj)
	}

	return nil
}

// Insert implements inserting an object or slice of objects in the database.
// Passed object must be a pointer to a struct with appropriate dgraph tags.
func (c client) Insert(ctx context.Context, obj any) error {
	// Validate struct before insertion
	if err := c.validateStruct(ctx, obj); err != nil {
		return err
	}

	if c.isLocal() {
		return c.mutateWithUniqueVerification(ctx, obj, true)
	}
	return c.process(ctx, obj, "Insert", func(tx *dg.TxnContext, obj any) ([]string, error) {
		return tx.MutateBasic(obj)
	})
}

// InsertRaw adds a new object or slice of objects to the database.
// The object must be a pointer to a struct with appropriate dgraph tags.
// This is a no-op for remote Dgraph clients. For local clients, this
// function mutates the Dgraph engine directly. No unique checks are performed.
// The `UID` field for any objects must be set using the Dgraph blank node
// prefix concept (e.g. "_:user1") to allow the engine to generate a UID for the object.
func (c client) InsertRaw(ctx context.Context, obj any) error {
	// Validate struct before insertion
	if err := c.validateStruct(ctx, obj); err != nil {
		return err
	}

	if c.isLocal() {
		// Validate object and update schema if autoSchema is enabled
		schemaObj, err := checkObject(obj)
		if err != nil {
			return err
		}
		if c.options.autoSchema {
			if err := c.UpdateSchema(ctx, schemaObj); err != nil {
				return err
			}
		}

		val := reflect.ValueOf(obj)
		var sliceValue reflect.Value

		mutations := []*api.Mutation{}
		// Handle pointer to slice
		if val.Kind() == reflect.Ptr && val.Elem().Kind() == reflect.Slice {
			sliceValue = val.Elem()
		} else if val.Kind() == reflect.Slice {
			// Direct slice
			sliceValue = val
		} else {
			// Single object - create a slice with one element
			valElem := val
			for valElem.Kind() == reflect.Ptr {
				valElem = valElem.Elem()
			}
			sliceType := reflect.SliceOf(valElem.Type())
			sliceValue = reflect.MakeSlice(sliceType, 1, 1)
			sliceValue.Index(0).Set(valElem)
		}

		// Recursively validate and prepare all structs (including nested ones)
		if err := validateAndPrepareStruct(sliceValue, "obj", make(map[uintptr]bool)); err != nil {
			return err
		}

		// iterate sliceValue and create mutations
		for i := 0; i < sliceValue.Len(); i++ {
			elem := sliceValue.Index(i)
			if elem.Kind() == reflect.Ptr {
				elem = elem.Elem()
			}
			if elem.Kind() != reflect.Struct {
				continue
			}

			data, err := json.Marshal(elem.Interface())
			if err != nil {
				return err
			}
			mutations = append(mutations, &api.Mutation{SetJson: data, CommitNow: false})
		}
		uidMap, err := c.engine.db0.Mutate(ctx, mutations)
		if err != nil {
			return err
		}

		// Replace blank node UIDs with actual generated UIDs in the original obj
		replaceUIDs(obj, uidMap)

		return nil
	} else {
		return c.process(ctx, obj, "Insert", func(tx *dg.TxnContext, obj any) ([]string, error) {
			return tx.MutateBasic(obj)
		})
	}
}

// Upsert implements inserting or updating an object or slice of objects in the database.
// Note that the struct tag `upsert` must be used. One or more predicates can be specified
// to be used for upserting. If none are specified, the first predicate with the `upsert` tag
// will be used.
// Note for local file clients, only the first struct field marked with `upsert` will be used
// if none are specified in the predicates argument.
func (c client) Upsert(ctx context.Context, obj any, predicates ...string) error {
	// Validate struct before upsert
	if err := c.validateStruct(ctx, obj); err != nil {
		return err
	}

	if c.isLocal() {
		var upsertPredicate string
		if len(predicates) > 0 {
			upsertPredicate = predicates[0]
			if len(predicates) > 1 {
				c.logger.V(1).Info("Multiple upsert predicates specified, local mode only supports one, using first of this list",
					"predicates", predicates)
			}
		}
		return c.upsert(ctx, obj, upsertPredicate)
	}
	return c.process(ctx, obj, "Upsert", func(tx *dg.TxnContext, obj any) ([]string, error) {
		return tx.Upsert(obj, predicates...)
	})
}

// Update implements updating an existing object in the database.
// Passed object must be a pointer to a struct.
func (c client) Update(ctx context.Context, obj any) error {
	// Validate struct before update
	if err := c.validateStruct(ctx, obj); err != nil {
		return err
	}

	if c.isLocal() {
		return c.mutateWithUniqueVerification(ctx, obj, false)
	}

	return c.process(ctx, obj, "Update", func(tx *dg.TxnContext, obj any) ([]string, error) {
		return tx.MutateBasic(obj)
	})
}

// Delete implements removing objects with the specified UIDs.
func (c client) Delete(ctx context.Context, uids []string) error {
	client, err := c.pool.get()
	if err != nil {
		c.logger.Error(err, "Failed to get client from pool")
		return err
	}
	defer c.pool.put(client)

	txn := dg.NewTxnContext(ctx, client).SetCommitNow()
	return txn.DeleteNode(uids...)
}

// Get implements retrieving a single object by its UID.
// Passed object must be a pointer to a struct.
func (c client) Get(ctx context.Context, obj any, uid string) error {
	err := checkPointer(obj)
	if err != nil {
		return err
	}
	client, err := c.pool.get()
	if err != nil {
		return err
	}
	defer c.pool.put(client)

	txn := dg.NewReadOnlyTxnContext(ctx, client)
	return txn.Get(obj).UID(uid).All(c.options.maxEdgeTraversal).Node()
}

// Returns a *dg.Query that can be further refined with filters, pagination, etc.
// The returned query will be limited to the maximum number of edges specified in the options.
func (c client) Query(ctx context.Context, model any) *dg.Query {
	client, err := c.pool.get()
	if err != nil {
		return nil
	}
	defer c.pool.put(client)

	txn := dg.NewReadOnlyTxnContext(ctx, client)
	return txn.Get(model).All(c.options.maxEdgeTraversal)
}

// UpdateSchema implements updating the Dgraph schema. Pass one or more
// objects that will be used to generate the schema.
func (c client) UpdateSchema(ctx context.Context, obj ...any) error {
	client, err := c.pool.get()
	if err != nil {
		c.logger.Error(err, "Failed to get client from pool")
		return err
	}
	defer c.pool.put(client)

	_, err = dg.CreateSchema(client, obj...)
	return err
}

// GetSchema implements retrieving the Dgraph schema.
func (c client) GetSchema(ctx context.Context) (string, error) {
	client, err := c.pool.get()
	if err != nil {
		c.logger.Error(err, "Failed to get client from pool")
		return "", err
	}
	defer c.pool.put(client)

	return dg.GetSchema(client)
}

// DropAll implements dropping all data and schema from the database.
func (c client) DropAll(ctx context.Context) error {
	client, err := c.pool.get()
	if err != nil {
		c.logger.Error(err, "Failed to get client from pool")
		return err
	}
	defer c.pool.put(client)

	return client.Alter(ctx, &api.Operation{DropAll: true})
}

// DropData implements dropping data from the database.
func (c client) DropData(ctx context.Context) error {
	client, err := c.pool.get()
	if err != nil {
		c.logger.Error(err, "Failed to get client from pool")
		return err
	}
	defer c.pool.put(client)

	return client.Alter(ctx, &api.Operation{DropOp: api.Operation_DATA})
}

// QueryRaw implements raw querying (DQL syntax) and optional variables.
func (c client) QueryRaw(ctx context.Context, q string, vars map[string]string) ([]byte, error) {
	if c.isLocal() {
		ns := c.engine.GetDefaultNamespace()
		resp, err := ns.QueryWithVars(ctx, q, vars)
		if err != nil {
			return nil, err
		}
		return resp.GetJson(), nil
	}

	client, err := c.pool.get()
	if err != nil {
		c.logger.Error(err, "Failed to get client from pool")
		return nil, err
	}
	defer c.pool.put(client)

	txn := dg.NewReadOnlyTxnContext(ctx, client)
	resp, err := txn.Txn().QueryWithVars(ctx, q, vars)
	if err != nil {
		return nil, err
	}
	return resp.GetJson(), nil
}

// Close releases resources used by the client.
func (c client) Close() {
	// Add nil check to prevent panic if pool is nil
	if c.pool != nil {
		c.pool.close()
	}
	if c.engine != nil {
		c.engine.Close()
	}
}

// DgraphClient returns a Dgraph client from the pool and a cleanup function to put it back.
//
// Usage:
//
//	client, cleanup, err := c.DgraphClient()
//	if err != nil { ... }
//	defer cleanup()
//
// The cleanup function is safe to call even if client is nil or err is not nil.
func (c client) DgraphClient() (client *dgo.Dgraph, cleanup func(), err error) {
	client, err = c.pool.get()
	cleanup = func() {
		if client != nil {
			c.pool.put(client)
		}
	}
	return client, cleanup, err
}

// validateAndPrepareStruct recursively validates and prepares structs for insertion
// by ensuring UID fields are in the correct format and DType fields are set
func validateAndPrepareStruct(val reflect.Value, path string, visited map[uintptr]bool) error {
	if !val.IsValid() {
		return nil
	}

	// Dereference pointers
	for val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return nil
		}
		// Prevent infinite recursion on circular references
		ptr := val.Pointer()
		if visited[ptr] {
			return nil
		}
		visited[ptr] = true
		val = val.Elem()
	}

	switch val.Kind() {
	case reflect.Struct:
		// Check and validate UID field
		uidField := val.FieldByName("UID")
		if uidField.IsValid() && uidField.Kind() == reflect.String {
			uidStr := uidField.String()
			if uidStr == "" {
				return fmt.Errorf("UID is empty at %s", path)
			}
			if !strings.HasPrefix(uidStr, "_:") {
				return fmt.Errorf("UID at %s is not in the form of _:<string>", path)
			}
		}

		// Check and set DType field if empty
		dtypeField := val.FieldByName("DType")
		if dtypeField.IsValid() && dtypeField.CanSet() {
			if dtypeField.Kind() == reflect.Slice && dtypeField.Len() == 0 {
				dtypeSlice := reflect.MakeSlice(reflect.TypeOf([]string{}), 1, 1)
				dtypeSlice.Index(0).SetString(val.Type().Name())
				dtypeField.Set(dtypeSlice)
			}
		}

		// Recursively process all struct fields
		for i := 0; i < val.NumField(); i++ {
			field := val.Field(i)
			fieldName := val.Type().Field(i).Name
			fieldPath := path + "." + fieldName
			if field.CanInterface() {
				if err := validateAndPrepareStruct(field, fieldPath, visited); err != nil {
					return err
				}
			}
		}

	case reflect.Slice, reflect.Array:
		for i := 0; i < val.Len(); i++ {
			indexPath := fmt.Sprintf("%s[%d]", path, i)
			if err := validateAndPrepareStruct(val.Index(i), indexPath, visited); err != nil {
				return err
			}
		}

	case reflect.Map:
		for _, key := range val.MapKeys() {
			mapVal := val.MapIndex(key)
			keyPath := fmt.Sprintf("%s[%v]", path, key.Interface())
			if err := validateAndPrepareStruct(mapVal, keyPath, visited); err != nil {
				return err
			}
		}
	}

	return nil
}

// replaceUIDs recursively walks through obj and replaces UID field values
// that match keys in the uidMap with their corresponding hex-encoded values
func replaceUIDs(obj any, uidMap map[string]uint64) {
	val := reflect.ValueOf(obj)
	replaceUIDsValue(val, uidMap, make(map[uintptr]bool))
}

func replaceUIDsValue(val reflect.Value, uidMap map[string]uint64, visited map[uintptr]bool) {
	if !val.IsValid() {
		return
	}

	// Dereference pointers
	for val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return
		}
		// Prevent infinite recursion on circular references
		ptr := val.Pointer()
		if visited[ptr] {
			return
		}
		visited[ptr] = true
		val = val.Elem()
	}

	switch val.Kind() {
	case reflect.Struct:
		// Check for UID field first
		uidField := val.FieldByName("UID")
		if uidField.IsValid() && uidField.CanSet() && uidField.Kind() == reflect.String {
			currentUID := uidField.String()
			if newUID, ok := uidMap[currentUID]; ok {
				uidField.SetString(fmt.Sprintf("%#x", newUID))
			}
		}

		// Recursively process all fields
		for i := 0; i < val.NumField(); i++ {
			field := val.Field(i)
			if field.CanInterface() {
				replaceUIDsValue(field, uidMap, visited)
			}
		}

	case reflect.Slice, reflect.Array:
		for i := 0; i < val.Len(); i++ {
			replaceUIDsValue(val.Index(i), uidMap, visited)
		}

	case reflect.Map:
		for _, key := range val.MapKeys() {
			mapVal := val.MapIndex(key)
			replaceUIDsValue(mapVal, uidMap, visited)
		}
	}
}

type clientPool struct {
	clients chan *dgo.Dgraph
	factory func() (*dgo.Dgraph, error)
	logger  logr.Logger
}

func newClientPool(size int, factory func() (*dgo.Dgraph, error), logger logr.Logger) *clientPool {
	return &clientPool{
		clients: make(chan *dgo.Dgraph, size),
		factory: factory,
		logger:  logger,
	}
}

func (p *clientPool) get() (*dgo.Dgraph, error) {
	// Try to reuse an existing client
	select {
	case client := <-p.clients:
		p.logger.V(2).Info("Reusing client from pool")
		return client, nil
	default:
		// No client in pool, fall through to create a new one
	}

	// Create a new client
	p.logger.V(2).Info("Creating new client")
	client, err := p.factory()
	if err != nil {
		p.logger.Error(err, "Failed to create new client")
	}
	return client, err
}

func (p *clientPool) put(client *dgo.Dgraph) {
	select {
	case p.clients <- client:
		p.logger.V(2).Info("Returned client to pool")
	default:
		// Pool is full, close the client
		p.logger.V(1).Info("Pool full, closing client")
		client.Close()
	}
}

func (p *clientPool) close() {
	count := 0
	for {
		select {
		case client, ok := <-p.clients:
			if !ok {
				return // channel is closed
			}
			client.Close()
			count++
		default:
			// No more clients in the channel
			p.logger.V(2).Info("Client pool closed", "closedConnections", count)
			return
		}
	}
}
