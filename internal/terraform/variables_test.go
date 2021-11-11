package terraform

import (
	"fmt"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/hashicorp/terraform/internal/addrs"
	"github.com/hashicorp/terraform/internal/tfdiags"

	"github.com/go-test/deep"
	"github.com/zclconf/go-cty/cty"
)

func TestVariables(t *testing.T) {
	tests := map[string]struct {
		Module   string
		Override map[string]cty.Value
		Want     InputValues
	}{
		"config only": {
			"vars-basic",
			nil,
			InputValues{
				"a": &InputValue{
					Value:      cty.StringVal("foo"),
					SourceType: ValueFromConfig,
					SourceRange: tfdiags.SourceRange{
						Filename: "testdata/vars-basic/main.tf",
						Start:    tfdiags.SourcePos{Line: 1, Column: 1, Byte: 0},
						End:      tfdiags.SourcePos{Line: 1, Column: 13, Byte: 12},
					},
				},
				"b": &InputValue{
					Value:      cty.ListValEmpty(cty.String),
					SourceType: ValueFromConfig,
					SourceRange: tfdiags.SourceRange{
						Filename: "testdata/vars-basic/main.tf",
						Start:    tfdiags.SourcePos{Line: 6, Column: 1, Byte: 55},
						End:      tfdiags.SourcePos{Line: 6, Column: 13, Byte: 67},
					},
				},
				"c": &InputValue{
					Value:      cty.MapValEmpty(cty.String),
					SourceType: ValueFromConfig,
					SourceRange: tfdiags.SourceRange{
						Filename: "testdata/vars-basic/main.tf",
						Start:    tfdiags.SourcePos{Line: 11, Column: 1, Byte: 113},
						End:      tfdiags.SourcePos{Line: 11, Column: 13, Byte: 125},
					},
				},
			},
		},

		"override": {
			"vars-basic",
			map[string]cty.Value{
				"a": cty.StringVal("bar"),
				"b": cty.ListVal([]cty.Value{
					cty.StringVal("foo"),
					cty.StringVal("bar"),
				}),
				"c": cty.MapVal(map[string]cty.Value{
					"foo": cty.StringVal("bar"),
				}),
			},
			InputValues{
				"a": &InputValue{
					Value:      cty.StringVal("bar"),
					SourceType: ValueFromCaller,
				},
				"b": &InputValue{
					Value: cty.ListVal([]cty.Value{
						cty.StringVal("foo"),
						cty.StringVal("bar"),
					}),
					SourceType: ValueFromCaller,
				},
				"c": &InputValue{
					Value: cty.MapVal(map[string]cty.Value{
						"foo": cty.StringVal("bar"),
					}),
					SourceType: ValueFromCaller,
				},
			},
		},

		"bools: config only": {
			"vars-basic-bool",
			nil,
			InputValues{
				"a": &InputValue{
					Value:      cty.True,
					SourceType: ValueFromConfig,
					SourceRange: tfdiags.SourceRange{
						Filename: "testdata/vars-basic-bool/main.tf",
						Start:    tfdiags.SourcePos{Line: 4, Column: 1, Byte: 177},
						End:      tfdiags.SourcePos{Line: 4, Column: 13, Byte: 189},
					},
				},
				"b": &InputValue{
					Value:      cty.False,
					SourceType: ValueFromConfig,
					SourceRange: tfdiags.SourceRange{
						Filename: "testdata/vars-basic-bool/main.tf",
						Start:    tfdiags.SourcePos{Line: 8, Column: 1, Byte: 214},
						End:      tfdiags.SourcePos{Line: 8, Column: 13, Byte: 226},
					},
				},
			},
		},

		"bools: override with string": {
			"vars-basic-bool",
			map[string]cty.Value{
				"a": cty.StringVal("foo"),
				"b": cty.StringVal("bar"),
			},
			InputValues{
				"a": &InputValue{
					Value:      cty.StringVal("foo"),
					SourceType: ValueFromCaller,
				},
				"b": &InputValue{
					Value:      cty.StringVal("bar"),
					SourceType: ValueFromCaller,
				},
			},
		},

		"bools: override with bool": {
			"vars-basic-bool",
			map[string]cty.Value{
				"a": cty.False,
				"b": cty.True,
			},
			InputValues{
				"a": &InputValue{
					Value:      cty.False,
					SourceType: ValueFromCaller,
				},
				"b": &InputValue{
					Value:      cty.True,
					SourceType: ValueFromCaller,
				},
			},
		},
	}

	for name, test := range tests {
		// Wrapped in a func so we can get defers to work
		t.Run(name, func(t *testing.T) {
			m := testModule(t, test.Module)
			fromConfig := DefaultVariableValues(m.Module.Variables)
			overrides := InputValuesFromCaller(test.Override)
			got := fromConfig.Override(overrides)

			if !got.Identical(test.Want) {
				t.Errorf("wrong result\ngot: %swant: %s", spew.Sdump(got), spew.Sdump(test.Want))
			}
			for _, problem := range deep.Equal(got, test.Want) {
				t.Errorf(problem)
			}
		})
	}
}

func TestCheckInputVariables(t *testing.T) {
	c := testModule(t, "input-variables")

	t.Run("No variables set", func(t *testing.T) {
		// No variables set
		diags := checkInputVariables(c.Module.Variables, nil)
		if !diags.HasErrors() {
			t.Fatal("check succeeded, but want errors")
		}

		// Required variables set, optional variables unset
		// This is still an error at this layer, since it's the caller's
		// responsibility to have already merged in any default values.
		diags = checkInputVariables(c.Module.Variables, InputValues{
			"foo": &InputValue{
				Value:      cty.StringVal("bar"),
				SourceType: ValueFromCLIArg,
			},
		})
		if !diags.HasErrors() {
			t.Fatal("check succeeded, but want errors")
		}
	})

	t.Run("All variables set", func(t *testing.T) {
		diags := checkInputVariables(c.Module.Variables, InputValues{
			"foo": &InputValue{
				Value:      cty.StringVal("bar"),
				SourceType: ValueFromCLIArg,
			},
			"bar": &InputValue{
				Value:      cty.StringVal("baz"),
				SourceType: ValueFromCLIArg,
			},
			"map": &InputValue{
				Value:      cty.StringVal("baz"), // okay because config has no type constraint
				SourceType: ValueFromCLIArg,
			},
			"object_map": &InputValue{
				Value: cty.MapVal(map[string]cty.Value{
					"uno": cty.ObjectVal(map[string]cty.Value{
						"foo": cty.StringVal("baz"),
						"bar": cty.NumberIntVal(2), // type = any
					}),
					"dos": cty.ObjectVal(map[string]cty.Value{
						"foo": cty.StringVal("bat"),
						"bar": cty.NumberIntVal(99), // type = any
					}),
				}),
				SourceType: ValueFromCLIArg,
			},
			"object_list": &InputValue{
				Value: cty.ListVal([]cty.Value{
					cty.ObjectVal(map[string]cty.Value{
						"foo": cty.StringVal("baz"),
						"bar": cty.NumberIntVal(2), // type = any
					}),
					cty.ObjectVal(map[string]cty.Value{
						"foo": cty.StringVal("bang"),
						"bar": cty.NumberIntVal(42), // type = any
					}),
				}),
				SourceType: ValueFromCLIArg,
			},
		})
		if diags.HasErrors() {
			t.Fatalf("unexpected errors: %s", diags.Err())
		}
	})

	t.Run("Invalid Complex Types", func(t *testing.T) {
		diags := checkInputVariables(c.Module.Variables, InputValues{
			"foo": &InputValue{
				Value:      cty.StringVal("bar"),
				SourceType: ValueFromCLIArg,
			},
			"bar": &InputValue{
				Value:      cty.StringVal("baz"),
				SourceType: ValueFromCLIArg,
			},
			"map": &InputValue{
				Value:      cty.StringVal("baz"), // okay because config has no type constraint
				SourceType: ValueFromCLIArg,
			},
			"object_map": &InputValue{
				Value: cty.MapVal(map[string]cty.Value{
					"uno": cty.ObjectVal(map[string]cty.Value{
						"foo": cty.StringVal("baz"),
						"bar": cty.NumberIntVal(2), // type = any
					}),
					"dos": cty.ObjectVal(map[string]cty.Value{
						"foo": cty.StringVal("bat"),
						"bar": cty.NumberIntVal(99), // type = any
					}),
				}),
				SourceType: ValueFromCLIArg,
			},
			"object_list": &InputValue{
				Value: cty.TupleVal([]cty.Value{
					cty.ObjectVal(map[string]cty.Value{
						"foo": cty.StringVal("baz"),
						"bar": cty.NumberIntVal(2), // type = any
					}),
					cty.ObjectVal(map[string]cty.Value{
						"foo": cty.StringVal("bang"),
						"bar": cty.StringVal("42"), // type = any, but mismatch with the first list item
					}),
				}),
				SourceType: ValueFromCLIArg,
			},
		})

		if diags.HasErrors() {
			t.Fatalf("unexpected errors: %s", diags.Err())
		}
	})
}

func TestPrepareFinalInputVariableValue(t *testing.T) {
	// This is just a concise way to define a bunch of *configs.Variable
	// objects to use in our tests below. We're only going to decode this
	// config, not fully evaluate it.
	cfgSrc := `
		variable "nullable_required" {
		}
		variable "nullable_optional_default_string" {
			default = "hello"
		}
		variable "nullable_optional_default_null" {
			default = null
		}
		variable "constrained_string_nullable_required" {
			type = string
		}
		variable "constrained_string_nullable_optional_default_string" {
			type    = string
			default = "hello"
		}
		variable "constrained_string_nullable_optional_default_bool" {
			type    = string
			default = true
		}
		variable "constrained_string_nullable_optional_default_null" {
			type    = string
			default = null
		}
		variable "required" {
			nullable = false
		}
		variable "optional_default_string" {
			nullable = false
			default  = "hello"
		}
		variable "constrained_string_required" {
			nullable = false
			type     = string
		}
		variable "constrained_string_optional_default_string" {
			nullable = false
			type     = string
			default  = "hello"
		}
		variable "constrained_string_optional_default_bool" {
			nullable = false
			type     = string
			default  = true
		}
	`
	cfg := testModuleInline(t, map[string]string{
		"main.tf": cfgSrc,
	})
	variableConfigs := cfg.Module.Variables

	tests := []struct {
		varName string
		given   cty.Value
		want    cty.Value
		wantErr string
	}{
		// nullable_required
		{
			"nullable_required",
			cty.NilVal,
			cty.UnknownVal(cty.DynamicPseudoType),
			`Required variable not set: The variable "nullable_required" is required, but is not set.`,
		},
		{
			"nullable_required",
			cty.NullVal(cty.DynamicPseudoType),
			cty.NullVal(cty.DynamicPseudoType),
			``, // "required" for a nullable variable means only that it must be set, even if it's set to null
		},
		{
			"nullable_required",
			cty.StringVal("ahoy"),
			cty.StringVal("ahoy"),
			``,
		},
		{
			"nullable_required",
			cty.UnknownVal(cty.String),
			cty.UnknownVal(cty.String),
			``,
		},

		// nullable_optional_default_string
		{
			"nullable_optional_default_string",
			cty.NilVal,
			cty.StringVal("hello"), // the declared default value
			``,
		},
		{
			"nullable_optional_default_string",
			cty.NullVal(cty.DynamicPseudoType),
			cty.NullVal(cty.DynamicPseudoType), // nullable variables can be really set to null, masking the default
			``,
		},
		{
			"nullable_optional_default_string",
			cty.StringVal("ahoy"),
			cty.StringVal("ahoy"),
			``,
		},
		{
			"nullable_optional_default_string",
			cty.UnknownVal(cty.String),
			cty.UnknownVal(cty.String),
			``,
		},

		// nullable_optional_default_null
		{
			"nullable_optional_default_null",
			cty.NilVal,
			cty.NullVal(cty.DynamicPseudoType), // the declared default value
			``,
		},
		{
			"nullable_optional_default_null",
			cty.NullVal(cty.String),
			cty.NullVal(cty.String), // nullable variables can be really set to null, masking the default
			``,
		},
		{
			"nullable_optional_default_null",
			cty.StringVal("ahoy"),
			cty.StringVal("ahoy"),
			``,
		},
		{
			"nullable_optional_default_null",
			cty.UnknownVal(cty.String),
			cty.UnknownVal(cty.String),
			``,
		},

		// constrained_string_nullable_required
		{
			"constrained_string_nullable_required",
			cty.NilVal,
			cty.UnknownVal(cty.String),
			`Required variable not set: The variable "constrained_string_nullable_required" is required, but is not set.`,
		},
		{
			"constrained_string_nullable_required",
			cty.NullVal(cty.DynamicPseudoType),
			cty.NullVal(cty.String), // the null value still gets converted to match the type constraint
			``,                      // "required" for a nullable variable means only that it must be set, even if it's set to null
		},
		{
			"constrained_string_nullable_required",
			cty.StringVal("ahoy"),
			cty.StringVal("ahoy"),
			``,
		},
		{
			"constrained_string_nullable_required",
			cty.UnknownVal(cty.String),
			cty.UnknownVal(cty.String),
			``,
		},

		// constrained_string_nullable_optional_default_string
		{
			"constrained_string_nullable_optional_default_string",
			cty.NilVal,
			cty.StringVal("hello"), // the declared default value
			``,
		},
		{
			"constrained_string_nullable_optional_default_string",
			cty.NullVal(cty.DynamicPseudoType),
			cty.NullVal(cty.String), // nullable variables can be really set to null, masking the default
			``,
		},
		{
			"constrained_string_nullable_optional_default_string",
			cty.StringVal("ahoy"),
			cty.StringVal("ahoy"),
			``,
		},
		{
			"constrained_string_nullable_optional_default_string",
			cty.UnknownVal(cty.String),
			cty.UnknownVal(cty.String),
			``,
		},

		// constrained_string_nullable_optional_default_bool
		{
			"constrained_string_nullable_optional_default_bool",
			cty.NilVal,
			cty.StringVal("true"), // the declared default value, automatically converted to match type constraint
			``,
		},
		{
			"constrained_string_nullable_optional_default_bool",
			cty.NullVal(cty.DynamicPseudoType),
			cty.NullVal(cty.String), // nullable variables can be really set to null, masking the default
			``,
		},
		{
			"constrained_string_nullable_optional_default_bool",
			cty.StringVal("ahoy"),
			cty.StringVal("ahoy"),
			``,
		},
		{
			"constrained_string_nullable_optional_default_bool",
			cty.UnknownVal(cty.String),
			cty.UnknownVal(cty.String),
			``,
		},

		// constrained_string_nullable_optional_default_null
		{
			"constrained_string_nullable_optional_default_null",
			cty.NilVal,
			cty.NullVal(cty.String),
			``,
		},
		{
			"constrained_string_nullable_optional_default_null",
			cty.NullVal(cty.DynamicPseudoType),
			cty.NullVal(cty.String),
			``,
		},
		{
			"constrained_string_nullable_optional_default_null",
			cty.StringVal("ahoy"),
			cty.StringVal("ahoy"),
			``,
		},
		{
			"constrained_string_nullable_optional_default_null",
			cty.UnknownVal(cty.String),
			cty.UnknownVal(cty.String),
			``,
		},

		// required
		{
			"required",
			cty.NilVal,
			cty.UnknownVal(cty.DynamicPseudoType),
			`Required variable not set: The variable "required" is required, but is not set.`,
		},
		{
			"required",
			cty.NullVal(cty.DynamicPseudoType),
			cty.UnknownVal(cty.DynamicPseudoType),
			`Required variable not set: The variable "required" is required, but the given value is null.`,
		},
		{
			"required",
			cty.StringVal("ahoy"),
			cty.StringVal("ahoy"),
			``,
		},
		{
			"required",
			cty.UnknownVal(cty.String),
			cty.UnknownVal(cty.String),
			``,
		},

		// optional_default_string
		{
			"optional_default_string",
			cty.NilVal,
			cty.StringVal("hello"), // the declared default value
			``,
		},
		{
			"optional_default_string",
			cty.NullVal(cty.DynamicPseudoType),
			cty.StringVal("hello"), // the declared default value
			``,
		},
		{
			"optional_default_string",
			cty.StringVal("ahoy"),
			cty.StringVal("ahoy"),
			``,
		},
		{
			"optional_default_string",
			cty.UnknownVal(cty.String),
			cty.UnknownVal(cty.String),
			``,
		},

		// constrained_string_required
		{
			"constrained_string_required",
			cty.NilVal,
			cty.UnknownVal(cty.String),
			`Required variable not set: The variable "constrained_string_required" is required, but is not set.`,
		},
		{
			"constrained_string_required",
			cty.NullVal(cty.DynamicPseudoType),
			cty.UnknownVal(cty.String),
			`Required variable not set: The variable "constrained_string_required" is required, but the given value is null.`,
		},
		{
			"constrained_string_required",
			cty.StringVal("ahoy"),
			cty.StringVal("ahoy"),
			``,
		},
		{
			"constrained_string_required",
			cty.UnknownVal(cty.String),
			cty.UnknownVal(cty.String),
			``,
		},

		// constrained_string_optional_default_string
		{
			"constrained_string_optional_default_string",
			cty.NilVal,
			cty.StringVal("hello"), // the declared default value
			``,
		},
		{
			"constrained_string_optional_default_string",
			cty.NullVal(cty.DynamicPseudoType),
			cty.StringVal("hello"), // the declared default value
			``,
		},
		{
			"constrained_string_optional_default_string",
			cty.StringVal("ahoy"),
			cty.StringVal("ahoy"),
			``,
		},
		{
			"constrained_string_optional_default_string",
			cty.UnknownVal(cty.String),
			cty.UnknownVal(cty.String),
			``,
		},

		// constrained_string_optional_default_bool
		{
			"constrained_string_optional_default_bool",
			cty.NilVal,
			cty.StringVal("true"), // the declared default value, automatically converted to match type constraint
			``,
		},
		{
			"constrained_string_optional_default_bool",
			cty.NullVal(cty.DynamicPseudoType),
			cty.StringVal("true"), // the declared default value, automatically converted to match type constraint
			``,
		},
		{
			"constrained_string_optional_default_bool",
			cty.StringVal("ahoy"),
			cty.StringVal("ahoy"),
			``,
		},
		{
			"constrained_string_optional_default_bool",
			cty.UnknownVal(cty.String),
			cty.UnknownVal(cty.String),
			``,
		},
	}

	for _, test := range tests {
		t.Run(fmt.Sprintf("%s %#v", test.varName, test.given), func(t *testing.T) {
			varAddr := addrs.InputVariable{Name: test.varName}.Absolute(addrs.RootModuleInstance)
			varCfg := variableConfigs[test.varName]
			if varCfg == nil {
				t.Fatalf("invalid variable name %q", test.varName)
			}

			t.Logf(
				"test case\nvariable:    %s\nconstraint:  %#v\ndefault:     %#v\nnullable:    %#v\ngiven value: %#v",
				varAddr,
				varCfg.Type,
				varCfg.Default,
				varCfg.Nullable,
				test.given,
			)

			got, diags := prepareFinalInputVariableValue(
				varAddr, test.given, tfdiags.SourceRangeFromHCL(varCfg.DeclRange), varCfg,
			)

			if test.wantErr != "" {
				if !diags.HasErrors() {
					t.Errorf("unexpected success\nwant error: %s", test.wantErr)
				} else if got, want := diags.Err().Error(), test.wantErr; got != want {
					t.Errorf("wrong error\ngot:  %s\nwant: %s", got, want)
				}
			} else {
				if diags.HasErrors() {
					t.Errorf("unexpected error\ngot: %s", diags.Err().Error())
				}
			}

			// NOTE: should still have returned some reasonable value even if there was an error
			if !test.want.RawEquals(got) {
				t.Fatalf("wrong result\ngot:  %#v\nwant: %#v", got, test.want)
			}
		})
	}
}
