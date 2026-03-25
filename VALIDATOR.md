# Validator Integration

ModusGraph supports struct validation through the `StructValidator` interface. This interface is
compatible with the popular
[github.com/go-playground/validator/v10](https://github.com/go-playground/validator) package, but
you can also provide your own implementation.

## The StructValidator Interface

```go
type StructValidator interface {
    StructCtx(ctx context.Context, s interface{}) error
}
```

## Usage

### Basic Setup with go-playground/validator

```go
import (
    "github.com/go-playground/validator/v10"
    mg "github.com/matthewmcneely/modusgraph"
)

// Create a validator instance (implements StructValidator)
validate := validator.New()

// Create a client with validator
client, err := mg.NewClient("file:///path/to/db", mg.WithValidator(validate))
if err != nil {
    log.Fatal(err)
}
defer client.Close()
```

### Using the Convenience Helper

```go
// NewValidator() returns a *validator.Validate with default settings
client, err := mg.NewClient("file:///path/to/db", mg.WithValidator(mg.NewValidator()))
```

### Custom Validator Implementation

You can implement your own validator:

```go
type MyValidator struct{}

func (v *MyValidator) StructCtx(ctx context.Context, s interface{}) error {
    // Your custom validation logic
    return nil
}

client, err := mg.NewClient("file:///path/to/db", mg.WithValidator(&MyValidator{}))
```

### Struct Validation Tags

Add validation tags to your struct fields:

```go
type User struct {
    UID     string   `json:"uid,omitempty"`
    Name    string   `json:"name,omitempty" validate:"required,min=2,max=100"`
    Email   string   `json:"email,omitempty" validate:"required,email"`
    Age     int      `json:"age,omitempty" validate:"gte=0,lte=150"`
    Status  string   `json:"status,omitempty" validate:"oneof=active inactive pending"`
    Tags    []string `json:"tags,omitempty"`
}
```

### Validation in Operations

The validator will automatically run before these operations:

- `Insert()` - Validates structs before insertion
- `InsertRaw()` - Validates structs before raw insertion
- `Update()` - Validates structs before update
- `Upsert()` - Validates structs before upsert

```go
user := &User{
    Name:   "John Doe",
    Email:  "john@example.com",
    Age:    30,
    Status: "active",
}

err := client.Insert(ctx, user)
if err != nil {
    // Handle validation errors
    log.Printf("Validation failed: %v", err)
    return
}
```

### Custom Validations

You can register custom validation functions:

```go
validate := validator.New()

// Register a custom validation
validate.RegisterValidation("custom", func(fl validator.FieldLevel) bool {
    return fl.Field().String() == "custom_value"
})

type CustomStruct struct {
    Field string `json:"field,omitempty" validate:"custom"`
}
```

### No Validation

If you don't want validation, simply don't provide a validator:

```go
// No validation will be performed
client, err := mg.NewClient("file:///path/to/db")
```

## Error Handling

When validation fails, the operation returns the validation error from the validator package. You
can check for specific validation errors:

```go
err := client.Insert(ctx, user)
if err != nil {
    // Check if it's a validation error
    if validationErrors, ok := err.(validator.ValidationErrors); ok {
        for _, e := range validationErrors {
            fmt.Printf("Field '%s' failed validation: %s\n", e.Field(), e.Tag())
        }
    }
}
```

## Supported Validation Tags

The validator package supports many built-in validation tags:

- `required` - Field must be present and non-empty
- `min`, `max` - Minimum/maximum value for numbers, length for strings
- `email` - Valid email format
- `url` - Valid URL format
- `oneof` - Value must be one of the specified options
- `gte`, `lte` - Greater than/equal to, less than/equal to
- And many more...

See the [validator documentation](https://github.com/go-playground/validator#usage) for a complete
list.

## Code Generation Interaction

The `validate` tag is also used by the `modusgraph-gen` code generator to detect edge cardinality.
When an edge field has `validate:"max=1"` or `validate:"len=1"`, the generator produces singular
edge accessors (`*Type` getter/setter) instead of slice accessors:

```go
type Film struct {
    UID      string     `json:"uid,omitempty"`
    DType    []string   `json:"dgraph.type,omitempty"`
    director []Director `json:"director,omitempty" validate:"max=1"`
}

// Generated:
// func (f *Film) Director() *Director { ... }
// func (f *Film) SetDirector(v *Director) { ... }
```

This provides a cleaner API when a relationship is conceptually one-to-one (or zero-to-one). The
validation tag enforces the cardinality at runtime, while the generated accessors express it in the
API surface. See the [Code Generation](README.md#code-generation) section in the README for full
details on private field support.
