package pretty

import (
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"text/tabwriter"
	"unicode/utf8"

	"github.com/kr/text"
)

var ConvertErrorStringObject bool = false

type formatter struct {
	v     reflect.Value
	force bool
	quote bool
	lazy  bool
}

// Formatter makes a wrapper, f, that will format x as go source with line
// breaks and tabs. Object f responds to the "%v" formatting verb when both the
// "#" and " " (space) flags are set, for example:
//
//     fmt.Sprintf("%# v", Formatter(x))
//
// If one of these two flags is not set, or any other verb is used, f will
// format x according to the usual rules of package fmt.
// In particular, if x satisfies fmt.Formatter, then x.Format will be called.
func Formatter(x interface{}) (f fmt.Formatter) {
	return formatter{v: reflect.ValueOf(x), quote: true}
}

// LazyFormatter works like Formatter but does not print fields with zero values.
func LazyFormatter(x interface{}) (f fmt.Formatter) {
	return formatter{v: reflect.ValueOf(x), quote: true, lazy: true}
}

func (fo formatter) String() string {
	return fmt.Sprint(fo.v) // unwrap it
}

func (fo formatter) passThrough(f fmt.State, c rune) {
	s := "%"
	for i := 0; i < 128; i++ {
		if f.Flag(i) {
			s += string(i)
		}
	}
	if w, ok := f.Width(); ok {
		s += fmt.Sprintf("%d", w)
	}
	if p, ok := f.Precision(); ok {
		s += fmt.Sprintf(".%d", p)
	}
	s += string(c)
	fmt.Fprintf(f, s, fo.v)
}

func (fo formatter) Format(f fmt.State, c rune) {
	if fo.force || c == 'v' && f.Flag('#') && f.Flag(' ') {
		w := tabwriter.NewWriter(f, 4, 4, 1, ' ', 0)
		p := &printer{tw: w, Writer: w, visited: make(map[visit]int), lazy: fo.lazy}
		p.printValue(fo.v, true, fo.quote)
		w.Flush()
		return
	}
	fo.passThrough(f, c)
}

type printer struct {
	io.Writer
	tw      *tabwriter.Writer
	visited map[visit]int
	depth   int
	lazy    bool
}

func (p *printer) indent() *printer {
	q := *p
	q.tw = tabwriter.NewWriter(p.Writer, 4, 4, 1, ' ', 0)
	q.Writer = text.NewIndentWriter(q.tw, []byte{'\t'})
	return &q
}

func (p *printer) printInline(v reflect.Value, x interface{}, showType bool) {
	if showType {
		p.writeType(v.Type())
		fmt.Fprintf(p, "(%#v)", x)
	} else {
		fmt.Fprintf(p, "%#v", x)
	}
}

// printValue must keep track of already-printed pointer values to avoid
// infinite recursion.
type visit struct {
	v   uintptr
	typ reflect.Type
}

type reflectValuesByOrder []reflect.Value

func (s reflectValuesByOrder) Len() int      { return len(s) }
func (s reflectValuesByOrder) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s reflectValuesByOrder) Less(i, j int) bool {
	si := stringValue(s[i])
	sj := stringValue(s[j])

	if si != sj {
		if si < sj {
			return true
		}

		return false
	}

	return false
}

func stringValue(v reflect.Value) string {
	stringMethod := v.MethodByName("String")
	if stringMethod.IsValid() {
		returnValues := stringMethod.Call(nil)
		if len(returnValues) == 1 && returnValues[0].IsValid() {
			return returnValues[0].String()
		}
	}

	return v.String()
}

func (p *printer) writeType(t reflect.Type) {
	switch t.Kind() {
	case reflect.Array:
		io.WriteString(p, "[")
		io.WriteString(p, string(t.Len()))
		io.WriteString(p, "]")
		p.writeType(t.Elem())
	case reflect.Chan:
		io.WriteString(p, "chan ")
		p.writeType(t.Elem())
	case reflect.Map:
		io.WriteString(p, "map[")
		p.writeType(t.Key())
		io.WriteString(p, "]")
		p.writeType(t.Elem())
	case reflect.Ptr:
		io.WriteString(p, "*")
		p.writeType(t.Elem())
	case reflect.Slice:
		io.WriteString(p, "[]")
		p.writeType(t.Elem())
	default:
		switch t.PkgPath() {
		// TODO We want to respect all our custom imports. https://gitlab.nethead.at/symflower/symflower/-/issues/203
		case "gitlab.nethead.at/symflower/symflower/model/ast", "gitlab.nethead.at/symflower/symflower/model/errors":
			io.WriteString(p, "model")
		}
		io.WriteString(p, t.String())
	}
}

func (p *printer) printValue(v reflect.Value, showType, quote bool) {
	if isNil(v) {
		io.WriteString(p, "nil")

		return
	}

	stringGoMethod := v.MethodByName("StringGo")
	if stringGoMethod.IsValid() {
		isPointerType := v.Type().Kind() == reflect.Pointer

		returnValues := stringGoMethod.Call([]reflect.Value{reflect.ValueOf(isPointerType)})
		if len(returnValues) == 1 && returnValues[0].IsValid() {
			p.fmtString(returnValues[0].String(), false)

			return
		}
	}

	switch v.Kind() {
	case reflect.Bool:
		p.printInline(v, v.Bool(), showType)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		p.printInline(v, v.Int(), showType)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		p.printInline(v, v.Uint(), showType)
	case reflect.Float32, reflect.Float64:
		p.printInline(v, v.Float(), showType)
	case reflect.Complex64, reflect.Complex128:
		fmt.Fprintf(p, "%#v", v.Complex())
	case reflect.String:
		p.fmtString(v.String(), quote)
	case reflect.Map:
		if v.IsNil() {
			io.WriteString(p, "nil")

			return
		}
		t := v.Type()
		if showType {
			p.writeType(t)
		}
		writeByte(p, '{')
		if Nonzero(v) {
			expand := !canInline(v.Type())
			pp := p
			if expand {
				writeByte(p, '\n')
				pp = p.indent()
			}
			keys := v.MapKeys()
			sort.Sort(reflectValuesByOrder(keys))
			for i := 0; i < v.Len(); i++ {
				showTypeInStruct := true
				k := keys[i]
				mv := v.MapIndex(k)
				pp.printValue(k, false, true)
				writeByte(pp, ':')
				if expand {
					writeByte(pp, '\t')
				}
				showTypeInStruct = t.Elem().Kind() == reflect.Interface
				pp.printValue(mv, showTypeInStruct, true)
				if expand {
					io.WriteString(pp, ",\n")
				} else if i < v.Len()-1 {
					io.WriteString(pp, ", ")
				}
			}
			if expand {
				pp.tw.Flush()
			}
		}
		writeByte(p, '}')
	case reflect.Struct:
		t := v.Type()
		if v.CanAddr() {
			addr := v.UnsafeAddr()
			vis := visit{addr, t}
			if vd, ok := p.visited[vis]; ok && vd < p.depth && p.depth > 40 {
				p.fmtString(t.String()+"{(CYCLIC REFERENCE)}", false)
				break // don't print v again
			}
			p.visited[vis] = p.depth
		}

		if showType {
			p.writeType(t)
		}
		writeByte(p, '{')
		if Nonzero(v) {
			expand := !canInline(v.Type())
			pp := p
			if expand {
				writeByte(p, '\n')
				pp = p.indent()
			}
			for i := 0; i < v.NumField(); i++ {
				showTypeInStruct := true
				if p.lazy && !Nonzero(v.Field(i)) {
					continue
				}
				if f := t.Field(i); f.Name != "" {
					io.WriteString(pp, f.Name)
					writeByte(pp, ':')
					if expand {
						writeByte(pp, '\t')
					}
					showTypeInStruct = labelType(f.Type)
				}
				pp.printValue(getField(v, i), showTypeInStruct, true)
				if expand {
					io.WriteString(pp, ",\n")
				} else if i < v.NumField()-1 {
					io.WriteString(pp, ", ")
				}
			}
			if expand {
				pp.tw.Flush()
			}
		}
		writeByte(p, '}')
	case reflect.Interface:
		switch e := v.Elem(); {
		case e.Kind() == reflect.Invalid:
			io.WriteString(p, "nil")
		case e.IsValid():
			pp := *p
			pp.depth++
			pp.printValue(e, showType, true)
		default:
			io.WriteString(p, v.Type().String())
			io.WriteString(p, "(nil)")
		}
	case reflect.Array, reflect.Slice:
		t := v.Type()
		if showType {
			p.writeType(t)
		}
		if v.Kind() == reflect.Slice && v.IsNil() && showType {
			io.WriteString(p, "(nil)")
			break
		}
		if v.Kind() == reflect.Slice && v.IsNil() {
			io.WriteString(p, "nil")
			break
		}

		if t.Elem().Kind() == reflect.Uint8 && utf8.Valid(v.Bytes()) {
			writeByte(p, '(')
			io.WriteString(p, strconv.Quote(string(v.Bytes())))
			writeByte(p, ')')
			break
		}

		writeByte(p, '{')
		expand := !canInline(v.Type())
		pp := p
		if expand {
			writeByte(p, '\n')
			pp = p.indent()
		}
		for i := 0; i < v.Len(); i++ {
			showTypeInSlice := t.Elem().Kind() == reflect.Interface
			pp.printValue(v.Index(i), showTypeInSlice, true)
			if expand {
				io.WriteString(pp, ",\n")
			} else if i < v.Len()-1 {
				io.WriteString(pp, ", ")
			}
		}
		if expand {
			pp.tw.Flush()
		}
		writeByte(p, '}')
		if t.Elem().Kind() == reflect.Uint8 {
			writeByte(p, ' ')
			io.WriteString(p, "/* ")
			io.WriteString(pp, strconv.Quote(string(v.Bytes())))
			io.WriteString(p, " */")
		}
	case reflect.Ptr:
		e := v.Elem()
		if !e.IsValid() {
			writeByte(p, '(')
			io.WriteString(p, v.Type().String())
			io.WriteString(p, ")(nil)")
		} else {
			if ConvertErrorStringObject && e.Type().PkgPath() == "errors" && e.Type().Name() == "errorString" {
				p.fmtString("errors.New(", false)
				p.printValue(e.FieldByName("s"), false, true)
				p.fmtString(")", false)
			} else {
				pp := *p
				pp.depth++
				writeByte(pp, '&')
				pp.printValue(e, true, true)
			}
		}
	case reflect.Chan:
		x := v.Pointer()
		if showType {
			writeByte(p, '(')
			p.writeType(v.Type())
			fmt.Fprintf(p, ")(%#v)", x)
		} else {
			fmt.Fprintf(p, "%#v", x)
		}
	case reflect.Func:
		io.WriteString(p, v.Type().String())
		io.WriteString(p, " {...}")
	case reflect.UnsafePointer:
		p.printInline(v, v.Pointer(), showType)
	case reflect.Invalid:
		io.WriteString(p, "nil")
	}
}

func isNil(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Interface:
		switch e := v.Elem(); {
		case e.Kind() == reflect.Invalid:
			return true
		case !e.IsValid():
			return true
		}
	case reflect.Array, reflect.Slice:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return true
		}
	case reflect.Ptr:
		e := v.Elem()
		if !e.IsValid() {
			return true
		}
	case reflect.Invalid:
		return true
	}

	return false
}

// neverInlinedTypeNames contains types that are never inlined specified by "<package-path>.<type-name>".
// TODO This should not be hardcoded. https://gitlab.nethead.at/symflower/symflower/-/issues/203
var neverInlinedTypeNames = map[string]bool{
	"gitlab.nethead.at/symflower/symflower/model/metrics.Symbol": true,
}

func canInline(t reflect.Type) bool {
	if neverInlinedTypeNames[t.PkgPath()+"."+t.Name()] {
		return false
	}

	switch t.Kind() {
	case reflect.Map:
		return !canExpand(t.Elem())
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			if canExpand(t.Field(i).Type) {
				return false
			}
		}
		return true
	case reflect.Interface:
		return false
	case reflect.Array, reflect.Slice:
		return !canExpand(t.Elem())
	case reflect.Ptr:
		return false
	case reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return false
	}
	return true
}

func canExpand(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Map, reflect.Struct,
		reflect.Interface, reflect.Array, reflect.Slice,
		reflect.Ptr:
		return true
	}
	return false
}

func labelType(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Array, reflect.Interface, reflect.Map, reflect.Slice, reflect.Struct:
		return true
	}
	return false
}

func (p *printer) fmtString(s string, quote bool) {
	if quote {
		s = strconv.Quote(s)
	}
	io.WriteString(p, s)
}

func writeByte(w io.Writer, b byte) {
	w.Write([]byte{b})
}

func getField(v reflect.Value, i int) reflect.Value {
	val := v.Field(i)
	if val.Kind() == reflect.Interface && !val.IsNil() {
		val = val.Elem()
	}
	return val
}
