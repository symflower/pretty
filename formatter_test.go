package pretty

import (
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unsafe"
)

type test struct {
	v interface{}
	s string
}

type LongStructTypeName struct {
	longFieldName      interface{}
	otherLongFieldName interface{}
}

type SA struct {
	t *T
	v T
}

type T struct {
	x, y int
}

type F int

func (f F) Format(s fmt.State, c rune) {
	fmt.Fprintf(s, "F(%d)", int(f))
}

var long = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

var gosyntax = []test{
	{nil, `nil`},
	{"", `""`},
	{"a", `"a"`},
	{1, "int(1)"},
	{1.0, "float64(1)"},
	{[]int(nil), "[]int(nil)"},
	{[0]int{}, "[0]int{}"},
	{complex(1, 0), "(1+0i)"},
	//{make(chan int), "(chan int)(0x1234)"},
	{unsafe.Pointer(uintptr(unsafe.Pointer(&long))), fmt.Sprintf("unsafe.Pointer(0x%02x)", uintptr(unsafe.Pointer(&long)))},
	{func(int) {}, "func(int) {...}"},
	{map[int]int{1: 1}, "map[int]int{1:1}"},
	{int32(1), "int32(1)"},
	{io.EOF, `&errors.errorString{s:"EOF"}`},
	{[]string{"a"}, `[]string{"a"}`},
	{
		[]string{long},
		`[]string{"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"}`,
	},
	{F(5), "pretty.F(5)"},
	{
		SA{&T{1, 2}, T{3, 4}},
		`pretty.SA{
    t:  &pretty.T{x:1, y:2},
    v:  pretty.T{x:3, y:4},
}`,
	},
	{
		map[int][]byte{1: {}},
		`map[int][]uint8{
    1:  {},
}`,
	},
	{
		map[int]T{1: {}},
		`map[int]pretty.T{
    1:  {},
}`,
	},
	{
		long,
		`"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"`,
	},
	{
		LongStructTypeName{
			longFieldName:      LongStructTypeName{},
			otherLongFieldName: long,
		},
		`pretty.LongStructTypeName{
    longFieldName:      pretty.LongStructTypeName{},
    otherLongFieldName: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789",
}`,
	},
	{
		&LongStructTypeName{
			longFieldName:      &LongStructTypeName{},
			otherLongFieldName: (*LongStructTypeName)(nil),
		},
		`&pretty.LongStructTypeName{
    longFieldName:      &pretty.LongStructTypeName{},
    otherLongFieldName: (*pretty.LongStructTypeName)(nil),
}`,
	},
	{
		[]LongStructTypeName{
			{nil, nil},
			{3, 3},
			{long, nil},
		},
		`[]pretty.LongStructTypeName{
    {},
    {
        longFieldName:      int(3),
        otherLongFieldName: int(3),
    },
    {
        longFieldName:      "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789",
        otherLongFieldName: nil,
    },
}`,
	},
	{
		[]interface{}{
			LongStructTypeName{nil, nil},
			[]byte{1, 2, 3},
			T{3, 4},
			LongStructTypeName{long, nil},
		},
		`[]interface {}{
    pretty.LongStructTypeName{},
    []uint8{0x1, 0x2, 0x3},
    pretty.T{x:3, y:4},
    pretty.LongStructTypeName{
        longFieldName:      "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789",
        otherLongFieldName: nil,
    },
}`,
	},
}

func TestGoSyntax(t *testing.T) {
	for _, tt := range gosyntax {
		s := fmt.Sprintf("%# v", Formatter(tt.v))
		if tt.s != s {
			t.Errorf("expected %q", tt.s)
			t.Errorf("got      %q", s)
			t.Errorf("expraw\n%s", tt.s)
			t.Errorf("gotraw\n%s", s)
		}
	}
}

type I struct {
	i int
	R interface{}
}

func (i *I) I() *I { return i.R.(*I) }

func TestCycle(t *testing.T) {
	type A struct{ *A }
	v := &A{}
	v.A = v

	// panics from stack overflow without cycle detection
	t.Logf("Example cycle:\n%# v", Formatter(v))

	p := &A{}
	s := fmt.Sprintf("%# v", Formatter([]*A{p, p}))
	if strings.Contains(s, "CYCLIC") {
		t.Errorf("Repeated address detected as cyclic reference:\n%s", s)
	}

	type R struct {
		i int
		*R
	}
	r := &R{
		i: 1,
		R: &R{
			i: 2,
			R: &R{
				i: 3,
			},
		},
	}
	r.R.R.R = r
	t.Logf("Example longer cycle:\n%# v", Formatter(r))

	r = &R{
		i: 1,
		R: &R{
			i: 2,
			R: &R{
				i: 3,
				R: &R{
					i: 4,
					R: &R{
						i: 5,
						R: &R{
							i: 6,
							R: &R{
								i: 7,
								R: &R{
									i: 8,
									R: &R{
										i: 9,
										R: &R{
											i: 10,
											R: &R{
												i: 11,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	// here be pirates
	r.R.R.R.R.R.R.R.R.R.R.R = r
	t.Logf("Example very long cycle:\n%# v", Formatter(r))

	i := &I{
		i: 1,
		R: &I{
			i: 2,
			R: &I{
				i: 3,
				R: &I{
					i: 4,
					R: &I{
						i: 5,
						R: &I{
							i: 6,
							R: &I{
								i: 7,
								R: &I{
									i: 8,
									R: &I{
										i: 9,
										R: &I{
											i: 10,
											R: &I{
												i: 11,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	iv := i.I().I().I().I().I().I().I().I().I().I()
	*iv = *i
	t.Logf("Example long interface cycle:\n%# v", Formatter(i))
}

type TestStringer struct {
	ordinal int
}

func (s *TestStringer) String() string {
	return strconv.Itoa(s.ordinal)
}

func TestReflectValuesByOrderLess(t *testing.T) {
	type testCase struct {
		Name string

		Values []any

		SortOrderIndices []uint
	}

	validate := func(t *testing.T, tc *testCase) {
		t.Run(tc.Name, func(t *testing.T) {
			if len(tc.Values) != len(tc.SortOrderIndices) {
				t.Log("values and sorted indices must have same length")
				t.FailNow()
			}

			reflects := make([]reflect.Value, len(tc.Values))
			for i, v := range tc.Values {
				reflects[i] = reflect.ValueOf(v)
			}

			// Sort which calls the "Less" function of the ordering definition.
			sort.Sort(reflectValuesByOrder(reflects))

			for i, v := range reflects {
				actualValue := v.Interface()
				expectedValue := tc.Values[tc.SortOrderIndices[i]]

				if !reflect.DeepEqual(actualValue, expectedValue) {
					t.Logf("index position: %d, expected: %v, actual: %v", i, expectedValue, actualValue)
					t.FailNow()
				}
			}
		})
	}

	validate(t, &testCase{
		Name: "Integer Types",

		Values: []any{1, 2, 3},

		SortOrderIndices: []uint{0, 1, 2}, // All are integers so the order does not change.
	})

	validate(t, &testCase{
		Name: "Mixed Types",

		Values: []any{uint(1), 2},

		SortOrderIndices: []uint{1, 0}, // The "<int Value>" is lexicographically lower than "<uint Value>" so it's sorted first.
	})

	validate(t, &testCase{
		Name: "Stringer Type",

		Values: []any{
			&TestStringer{
				ordinal: 3,
			},
			&TestStringer{
				ordinal: 2,
			},
			&TestStringer{
				ordinal: 1,
			},
		},

		SortOrderIndices: []uint{2, 1, 0}, // If the type has a "String" method, that one's result is used for sorting.
	})
}
