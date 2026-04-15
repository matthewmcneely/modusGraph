/*
 * SPDX-FileCopyrightText: © 2017-2026 Istari Digital, Inc.
 * SPDX-License-Identifier: Apache-2.0
 */

package modusgraph

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type AllTags struct {
	Name          string `json:"name,omitempty" dgraph:"index=exact upsert"`
	Email         string `json:"email,omitempty" dgraph:"index=hash unique upsert"`
	UserID        string `json:"user_id,omitempty" dgraph:"predicate=my_user_id index=hash unique upsert"`
	NoUpsert      string `json:"no_upsert,omitempty" dgraph:"index=term"`
	WithJson      string `json:"with_json" dgraph:"upsert"`
	WithNoTags    string
	PredicateOnly string   `json:"pred_only" dgraph:"predicate=pred_only upsert"`
	MultiTag      string   `json:"multi_tag" dgraph:"index=term,upsert ,unique"`
	UID           string   `json:"uid,omitempty"`
	EmployeeID    int      `json:"employee_id,omitempty" dgraph:"index=int unique"`
	DType         []string `json:"dgraph.type,omitempty"`
}

func TestStructUpsertTagsProcessing(t *testing.T) {

	tests := []struct {
		name      string
		input     any
		expected  []string
		firstOnly bool
	}{
		{
			"Single upsert field",
			struct {
				Name string `json:"name" dgraph:"upsert"`
			}{},
			[]string{"name"},
			false,
		},
		{
			"Index and upsert",
			struct {
				Name string `json:"name" dgraph:"index=exact upsert"`
			}{},
			[]string{"name"},
			false,
		},
		{
			"Predicate override",
			struct {
				UserID string `json:"user_id" dgraph:"predicate=my_user_id upsert"`
			}{},
			[]string{"my_user_id"},
			false,
		},
		{
			"Json fallback",
			struct {
				WithJson string `json:"with_json" dgraph:"upsert"`
			}{},
			[]string{"with_json"},
			false,
		},
		{
			"No upsert",
			struct {
				Desc string `json:"desc" dgraph:"index=term"`
			}{},
			[]string{},
			false,
		},
		{
			"Multiple upserts",
			struct {
				A string `json:"a" dgraph:"upsert"`
				B string `json:"b" dgraph:"upsert"`
			}{},
			[]string{"a", "b"},
			false,
		},
		{
			"MultiTag comma",
			struct {
				MultiTag string `json:"multi_tag" dgraph:"index=term,upsert,unique"`
			}{},
			[]string{"multi_tag"},
			false,
		},
		{
			"With format issues",
			struct {
				Count int `json:"count" dgraph:"index=int    upsert"`
			}{},
			[]string{"count"},
			false,
		},
		{
			"AllTags struct",
			AllTags{},
			[]string{"name", "email", "my_user_id", "with_json", "pred_only", "multi_tag"},
			false,
		},
		{
			"First only",
			struct {
				A string `json:"a" dgraph:"upsert"`
				B string `json:"b" dgraph:"upsert"`
			}{},
			[]string{"a"},
			true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			predMap := getUpsertPredicates(tc.input, tc.firstOnly)
			preds := make([]string, 0, len(predMap))
			for k := range predMap {
				preds = append(preds, k)
			}
			sort.Strings(preds)
			sort.Strings(tc.expected)
			require.Equal(t, tc.expected, preds)
		})
	}
}

func TestStructUniqueTagsProcessing(t *testing.T) {

	tests := []struct {
		name     string
		input    any
		expected []string
	}{
		{
			"Single unique field",
			struct {
				Name string `json:"name" dgraph:"unique"`
			}{},
			[]string{"name"},
		},
		{
			"Multiple unique fields",
			struct {
				Name  string `json:"name" dgraph:"unique"`
				Email string `json:"email" dgraph:"unique"`
			}{},
			[]string{"name", "email"},
		},
		{
			"No unique fields",
			struct {
				Name string `json:"name"`
			}{},
			[]string{},
		},
		{
			"AllTags struct",
			AllTags{},
			[]string{"email", "my_user_id", "multi_tag", "employee_id"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			predMap := getUniquePredicates(tc.input)
			preds := make([]string, 0, len(predMap))
			for k := range predMap {
				preds = append(preds, k)
			}
			sort.Strings(preds)
			sort.Strings(tc.expected)
			require.Equal(t, tc.expected, preds)
		})
	}
}

func TestGenerateFilterQuery(t *testing.T) {
	obj := &AllTags{
		Email:      "alice@example.com",
		UserID:     "123",
		EmployeeID: 123,
		MultiTag:   "multi_tag",
		DType:      []string{"User"},
	}
	predicates := getUniquePredicates(obj)
	query, vars := generateUniquePredicateQuery(predicates, obj.DType[0])

	require.True(t, strings.Index(query, "query q(") == 0)
	require.Contains(t, query, "$email: string")
	require.Contains(t, query, "$my_user_id: string")
	require.Contains(t, query, "$employee_id: int")
	require.Contains(t, query, "func: type(User)")
	require.Contains(t, query, "eq(email, $email)")
	require.Contains(t, query, "eq(my_user_id, $my_user_id)")
	require.Contains(t, query, "eq(employee_id, $employee_id)")
	require.Contains(t, query, "OR")
	require.Contains(t, query, "uid")

	require.Equal(t, "alice@example.com", vars["$email"])
	require.Equal(t, "123", vars["$my_user_id"])
	require.Equal(t, "123", vars["$employee_id"])
	require.Equal(t, "multi_tag", vars["$multi_tag"])

	//fmt.Println(query)
}

// privateFieldEntity simulates a generated entity with private fields
// and a ValidateWith mirror struct. This tests the engine's SelfValidator
// dispatch without requiring actual code generation.
type privateFieldEntity struct {
	UID   string   `json:"uid,omitempty"`
	DType []string `json:"dgraph.type,omitempty"`
	name  string
	year  int
	score float64
	ok    bool
}

// privateFieldEntityReflectable is the all-exported shadow struct for
// privateFieldEntity. dgman's reflectwalk can traverse this normally.
type privateFieldEntityReflectable struct {
	UID   string   `json:"uid,omitempty"`
	DType []string `json:"dgraph.type,omitempty"`
	Name  string   `json:"name,omitempty"`
	Year  int      `json:"year,omitempty"`
	Score float64  `json:"score,omitempty"`
	Ok    bool     `json:"ok,omitempty"`
}

func (e *privateFieldEntity) ToReflectable() any {
	return &privateFieldEntityReflectable{
		UID:   e.UID,
		DType: e.DType,
		Name:  e.name,
		Year:  e.year,
		Score: e.score,
		Ok:    e.ok,
	}
}

func (e *privateFieldEntity) FromReflectable(model any) {
	s := model.(*privateFieldEntityReflectable)
	e.UID = s.UID
	e.DType = s.DType
}

func (e *privateFieldEntity) ValidateWith(ctx context.Context, v StructValidator) error {
	type mirror struct {
		Name  string  `validate:"required,min=2"`
		Year  int     `validate:"gte=1900,lte=2100"`
		Score float64 `validate:"gte=0,lte=100"`
		Ok    bool
	}
	return v.StructCtx(ctx, mirror{
		Name:  e.name,
		Year:  e.year,
		Score: e.score,
		Ok:    e.ok,
	})
}

// customTagEntity tests that custom validator tags work through ValidateWith
type customTagEntity struct {
	UID   string   `json:"uid,omitempty"`
	DType []string `json:"dgraph.type,omitempty"`
	code  string
}

func (e *customTagEntity) ValidateWith(ctx context.Context, v StructValidator) error {
	type mirror struct {
		Code string `validate:"required,studio_code"`
	}
	return v.StructCtx(ctx, mirror{Code: e.code})
}

func TestSelfValidatorDispatch(t *testing.T) {
	validate := validator.New()
	ctx := context.Background()

	t.Run("ValidPrivateFields", func(t *testing.T) {
		e := &privateFieldEntity{name: "Test Studio", year: 2000, score: 85.5, ok: true}
		err := e.ValidateWith(ctx, validate)
		require.NoError(t, err)
	})

	t.Run("InvalidName_TooShort", func(t *testing.T) {
		e := &privateFieldEntity{name: "X", year: 2000, score: 50}
		err := e.ValidateWith(ctx, validate)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Name")
	})

	t.Run("InvalidName_Empty", func(t *testing.T) {
		e := &privateFieldEntity{name: "", year: 2000, score: 50}
		err := e.ValidateWith(ctx, validate)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Name")
	})

	t.Run("InvalidYear_TooLow", func(t *testing.T) {
		e := &privateFieldEntity{name: "Test", year: 1800, score: 50}
		err := e.ValidateWith(ctx, validate)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Year")
	})

	t.Run("InvalidYear_TooHigh", func(t *testing.T) {
		e := &privateFieldEntity{name: "Test", year: 2200, score: 50}
		err := e.ValidateWith(ctx, validate)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Year")
	})

	t.Run("InvalidScore_Negative", func(t *testing.T) {
		e := &privateFieldEntity{name: "Test", year: 2000, score: -1}
		err := e.ValidateWith(ctx, validate)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Score")
	})

	t.Run("InvalidScore_TooHigh", func(t *testing.T) {
		e := &privateFieldEntity{name: "Test", year: 2000, score: 101}
		err := e.ValidateWith(ctx, validate)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Score")
	})

	t.Run("BoundaryValues", func(t *testing.T) {
		// Exact boundary: year=1900, score=0, name=2 chars
		e := &privateFieldEntity{name: "AB", year: 1900, score: 0}
		err := e.ValidateWith(ctx, validate)
		require.NoError(t, err)

		// Upper boundary: year=2100, score=100
		e2 := &privateFieldEntity{name: "AB", year: 2100, score: 100}
		err = e2.ValidateWith(ctx, validate)
		require.NoError(t, err)
	})
}

func TestSelfValidatorWithCustomValidator(t *testing.T) {
	validate := validator.New()

	// Register custom validator: studio_code must be 3 uppercase letters + 3 digits
	err := validate.RegisterValidation("studio_code", func(fl validator.FieldLevel) bool {
		code := fl.Field().String()
		if len(code) != 6 {
			return false
		}
		for i := 0; i < 3; i++ {
			if code[i] < 'A' || code[i] > 'Z' {
				return false
			}
		}
		for i := 3; i < 6; i++ {
			if code[i] < '0' || code[i] > '9' {
				return false
			}
		}
		return true
	})
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("ValidCustomCode", func(t *testing.T) {
		e := &customTagEntity{code: "ABC123"}
		err := e.ValidateWith(ctx, validate)
		require.NoError(t, err)
	})

	t.Run("InvalidCustomCode_WrongFormat", func(t *testing.T) {
		e := &customTagEntity{code: "abc123"}
		err := e.ValidateWith(ctx, validate)
		require.Error(t, err)
	})

	t.Run("InvalidCustomCode_TooShort", func(t *testing.T) {
		e := &customTagEntity{code: "AB12"}
		err := e.ValidateWith(ctx, validate)
		require.Error(t, err)
	})

	t.Run("InvalidCustomCode_Empty", func(t *testing.T) {
		e := &customTagEntity{code: ""}
		err := e.ValidateWith(ctx, validate)
		require.Error(t, err) // required tag + custom
	})
}

func TestValidateOneDispatchesSelfValidator(t *testing.T) {
	validate := validator.New()
	c := client{options: clientOptions{validator: validate}}
	ctx := context.Background()

	t.Run("SelfValidatorEntity_Valid", func(t *testing.T) {
		e := &privateFieldEntity{name: "Test Studio", year: 2000, score: 50}
		val := reflect.ValueOf(e)
		err := c.validateOne(ctx, val)
		require.NoError(t, err)
	})

	t.Run("SelfValidatorEntity_Invalid", func(t *testing.T) {
		e := &privateFieldEntity{name: "", year: 3000, score: -1}
		val := reflect.ValueOf(e)
		err := c.validateOne(ctx, val)
		require.Error(t, err)
	})

	t.Run("RegularEntity_FallsBackToStructCtx", func(t *testing.T) {
		// AllTags does NOT implement SelfValidator — should use StructCtx
		e := &AllTags{Email: "valid@example.com"}
		val := reflect.ValueOf(e)
		err := c.validateOne(ctx, val)
		// Should not panic (AllTags has only exported fields)
		// May fail validation but should not panic
		_ = err
	})
}

func TestHasReflectable(t *testing.T) {
	t.Run("ToReflectable", func(t *testing.T) {
		e := &privateFieldEntity{UID: "0x1", name: "Studio A", year: 2000, score: 85.5, ok: true}
		r := e.ToReflectable()
		s, ok := r.(*privateFieldEntityReflectable)
		require.True(t, ok)
		assert.Equal(t, "0x1", s.UID)
		assert.Equal(t, "Studio A", s.Name)
		assert.Equal(t, 2000, s.Year)
		assert.Equal(t, 85.5, s.Score)
		assert.True(t, s.Ok)
	})

	t.Run("FromReflectable", func(t *testing.T) {
		e := &privateFieldEntity{name: "Studio A"}
		shadow := &privateFieldEntityReflectable{UID: "0xabc", DType: []string{"privateFieldEntity"}}
		e.FromReflectable(shadow)
		assert.Equal(t, "0xabc", e.UID)
		assert.Equal(t, []string{"privateFieldEntity"}, e.DType)
		// Private fields unchanged.
		assert.Equal(t, "Studio A", e.name)
	})
}
