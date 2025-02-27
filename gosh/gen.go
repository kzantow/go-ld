package gosh

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"

	. "github.com/dave/jennifer/jen"
	"github.com/gertd/go-pluralize"
	"mvdan.cc/gofumpt/format"

	"github.com/kzantow/go-ld"
)

func Generate(opts ...Option) {
	g := &generator{
		pkgName:           "model",
		outputValidations: true,
		classes:           map[string]*Class{},
		contexts:          map[string]map[string]any{},
		nameToIRI:         map[string]string{},
		iriToType:         map[string]*Class{},
		renameFunc: func(typ NameType, name string) string {
			return ""
		},
	}
	for _, opt := range opts {
		opt(g)
	}
	g.Generate()
}

type Option func(*generator)

func EnableLog() Option {
	return func(generator *generator) {
		logEnabled = true
	}
}

func RenameFunc(fn func(typ NameType, name string) string) Option {
	return func(generator *generator) {
		generator.renameFunc = fn
	}
}

func OutputFile(path string) Option {
	return func(generator *generator) {
		generator.outputFile = strings.TrimSuffix(path, ".go")
	}
}

func PackageName(pkg string) Option {
	return func(generator *generator) {
		generator.pkgName = pkg
	}
}

func JsonLDContext(url string) Option {
	return func(generator *generator) {
		if generator.contexts[url] != nil {
			panic("duplicate contexts registered: " + url)
		}
		contents := fetch(url)
		var ctx map[string]any
		must(json.Unmarshal(contents, &ctx))
		generator.contexts[url] = ctx
	}
}

func SHACLTypes(url string) Option {
	return func(generator *generator) {
		classes, namedIndividuals := ParseSHACLFromUrl(url)
		for k, v := range classes {
			if generator.classes[k] != nil {
				panic("duplicate class iri defined: " + k)
			}
			generator.classes[k] = v
		}
		generator.namedIndividuals = append(generator.namedIndividuals, namedIndividuals...)
	}
}

type NameType int

const (
	NameTypeType NameType = iota
	NameTypeField
	NameTypeFunc
)

type renameFunc func(typ NameType, name string) string

var ldImport = reflect.TypeOf(ld.Context{}).PkgPath()

type generator struct {
	pkgName           string
	contexts          map[string]map[string]any
	classes           map[string]*Class // classes by IRI
	namedIndividuals  []*Individual
	outputFile        string
	outputValidations bool
	nameToIRI         map[string]string
	iriToType         map[string]*Class
	renameFunc        renameFunc
}

func (g *generator) Generate() {
	f := g.newCode()

	totalTypes := 0
	totalProps := 0

	// get all the final type names so we can output things alphabetically
	for _, c := range g.classes {
		totalTypes++
		renamed := g.className(c.IRI)
		c.GoName = renamed
		g.nameToIRI[renamed] = c.IRI
		g.iriToType[c.IRI] = c

		totalProps += len(c.Properties)
	}

	// set all prop go names
	for _, c := range g.classes {
		totalProps += len(c.Properties)
		for _, p := range c.Properties {
			p.GoName = g.propName(c, p)
		}
	}

	slices.SortFunc(g.namedIndividuals, func(a, b *Individual) int {
		return strings.Compare(a.IRI, b.IRI)
	})

	log("SUMMARY -- classes:", totalTypes, ", properties:", totalProps, ", individuals:", len(g.namedIndividuals))

	// sort output alphabetically by type name, the names have already been replaced
	for _, name := range keys(g.nameToIRI) {
		iri := g.nameToIRI[name]
		c := g.iriToType[iri]

		if !g.isEnum(c.IRI) {
			f.Type().Id(interfacePrefix + name).Interface(
				Id(viewPrefix + name).Params().Op("*").Id(name),
			)
		}

		if c.Comment != "" {
			f.Comment(prefixWith(strings.ReplaceAll(c.Comment, "\\n", " "), name))
		}

		// append the actual struct
		f.Type().Id(name).Struct(
			g.typeFields(c)...,
		)

		// append the interface for this struct, can be extended
		if !g.isEnum(c.IRI) {
			f.Func().Params(Id("o").Op("*").Id(name)).Id(viewPrefix + name).Params().Op("*").Id(name).Block(
				Return(Id("o")),
			)
		}

		g.appendNamedIndividualsForType(f, g.namedIndividuals, c.IRI)

		// append the list type for this struct
		if g.isObject(c.IRI) && !g.isEnum(c.IRI) {
			g.appendListType(f, c)
		}
	}

	// append external IRI type
	g.appendExternalIRI(f)

	// append cast functions
	g.appendCastFuncs(f)

	// append utilities, like typeIter
	g.appendUtils(f)

	// append context registration
	g.appendContextRegistration(f)

	log()

	if g.outputFile != "" {
		must(os.WriteFile(g.outputFile+".go", formattedSource(f), 0777))
	}

	if g.outputValidations && g.outputFile != "" {
		f = g.newCode()
	}

	g.appendValidations(f)
	if g.outputFile != "" {
		must(os.WriteFile(g.outputFile+"_validations.go", []byte(f.GoString()), 0777))
	}

	if g.outputFile == "" {
		_, _ = fmt.Fprintf(os.Stdout, "// Generated:\n%#v", f.GoString())
	}
}

func formattedSource(f *File) []byte {
	src := []byte(f.GoString())
	// fix the ld.Context{}.Register() argument list
	src = regexp.MustCompile(`{}, `).ReplaceAll(src, []byte("{},\n"))
	src = regexp.MustCompile(`{}\)`).ReplaceAll(src, []byte("{},\n)"))
	src = get(format.Source(src, format.Options{}))
	return src
}

func (g *generator) newCode() *File {
	f := NewFile(g.pkgName)
	f.ImportNames(map[string]string{
		"time":   "time",
		ldImport: "ld",
	})
	return f
}

func (g *generator) typeFields(c *Class) []Code {
	out := []Code{
		Id(ld.GoTypeField).Qual(ldImport, "Type").Tag(map[string]string{
			ld.GoIriTagName: c.IRI,
		}),
	}
	out = append(out, g.embedSupertypeOrID(c)...)
	out = append(out, g.addDirectProperties(c)...)
	return out
}

func (g *generator) propName(c *Class, p *Property) string {
	iri := p.IRI

	iri = cleanIRI(iri)
	parts := strings.Split(iri, "/")
	slices.Reverse(parts)
	name := ""
	for i, part := range parts {
		name = upperFirst(part) + name
		if requireMultipleSegments && i < 1 {
			continue
		}
		break
	}
	if g.isList(c, p) {
		name = g.pluralize(name)
	}
	renamed := g.renameFunc(NameTypeField, name)
	if renamed != "" {
		return renamed
	}
	return name
}

func (g *generator) isEnum(typeIRI string) bool {
	c := g.iriToType[typeIRI]
	if c != nil {
		return c.ParentIRI == "" && len(c.Properties) == 0
	}
	return false
}

func (g *generator) isList(_ *Class, p *Property) bool {
	return p.MaxCount != 1
}

func (g *generator) embedSupertypeOrID(c *Class) []Code {
	var out []Code
	if c.ParentIRI != "" {
		p := g.iriToType[c.ParentIRI]
		if p == nil {
			panic("Unknown parent: " + c.ParentIRI)
		}
		out = append(out, Id(p.GoName))
	} else {
		out = append(out, Id(ld.GoIdField).Id("string").Tag(map[string]string{
			ld.GoIriTagName: ld.JsonIdProp,
		}))
	}
	return out
}

func (g *generator) addDirectProperties(c *Class) []Code {
	var out []Code
	for _, p := range c.Properties {
		name := g.fieldName(c, p)
		if p.Comment != "" {
			out = append(out, Comment(prefixWith(strings.ReplaceAll(p.Comment, "\\n", " "), name)))
		}
		out = append(out, Id(name).Add(g.fieldType(c, p)).Tag(map[string]string{
			ld.GoIriTagName: p.IRI,
		}))
	}
	return out
}

func prefixWith(text string, prefix string) string {
	prefix = strings.TrimSpace(prefix) + " "
	if !strings.HasPrefix(text, prefix) {
		text = prefix + text
	}
	return text
}

func (g *generator) fieldType(c *Class, p *Property) Code {
	isObj := g.isObject(p.TypeIRI)
	isList := g.isList(c, p)
	isEnum := isObj && g.isEnum(p.TypeIRI)

	pkg, typ := g.baseType(c, p)
	if isObj && !isEnum {
		if isList {
			typ += listSuffix
		} else {
			typ = interfacePrefix + typ
		}
	}
	t := Id(typ)
	if pkg != "" {
		t = Qual(pkg, typ)
	}
	switch {
	case !isObj && isList, isObj && isEnum && isList:
		t = Index().Add(t)
	}
	return t
}

func (g *generator) isObject(iri string) bool {
	return g.iriToType[iri] != nil
}

func (g *generator) baseType(c *Class, p *Property) (pkg string, typ string) {
	iri := cleanIRI(p.TypeIRI)
	switch iri {
	case "http://www.w3.org/2001/XMLSchema#string", "http://www.w3.org/2001/XMLSchema#anyURI":
		return "", "string"

	case "http://www.w3.org/2001/XMLSchema#integer", "http://www.w3.org/2001/XMLSchema#positiveInteger", "http://www.w3.org/2001/XMLSchema#nonNegativeInteger":
		return "", "int"

	case "http://www.w3.org/2001/XMLSchema#boolean":
		return "", "bool"

	case "http://www.w3.org/2001/XMLSchema#decimal":
		return "", "float64"

	case "http://www.w3.org/2001/XMLSchema#dateTime", "http://www.w3.org/2001/XMLSchema#dateTimeStamp":
		return "time", "Time"
	}

	c = g.iriToType[iri]
	if c == nil {
		panic("Unknown type for IRI: " + iri)
	}
	renamed := g.renameFunc(NameTypeType, c.GoName)
	if renamed == "" {
		renamed = c.GoName
	}
	parts := strings.Split(renamed, ".")
	if len(parts) > 1 {
		return strings.Join(parts[0:len(parts)-1], "."), parts[len(parts)-1]
	}
	return "", renamed
}

func (g *generator) fieldName(_ *Class, p *Property) string {
	renamed := g.renameFunc(NameTypeField, p.GoName)
	if renamed == "" {
		renamed = p.GoName
	}
	return renamed
}

func (g *generator) className(iri string) string {
	iri = strings.Trim(iri, "<>")
	parts := strings.Split(iri, "/")
	slices.Reverse(parts)
	name := ""
	for i, part := range parts {
		name = upperFirst(part) + name
		if requireMultipleSegments && i < 1 {
			continue
		}
		if in(g.iriToType, name) {
			continue
		}
		break
	}
	renamed := g.renameFunc(NameTypeType, name)
	if renamed != "" {
		return renamed
	}
	return name
}

func (g *generator) appendListType(f *File, c *Class) {
	if g.isEnum(c.IRI) {
		return
	}
	listType := g.className(c.IRI) + listSuffix

	// append the list type
	f.Type().Id(listType).Id("[]" + g.interfaceName(c.IRI))

	// append all the typed getters
	g.appendListTypeGetters(f, listType, c)
}

func (g *generator) appendListTypeGetters(f *File, listTypeName string, listTyp *Class) {
	for _, name := range keys(g.nameToIRI) {
		iri := g.nameToIRI[name]
		if g.isEnum(iri) {
			continue
		}
		c := g.iriToType[iri]
		if c == listTyp || g.isSubtypeOf(listTyp, c) {
			f.Func().Params(Id("o").Op("*").Id(listTypeName)).Id(g.className(c.IRI)+iterSuffix).Params().Qual("iter", "Seq2").Index(Id(g.interfaceName(listTyp.IRI)).Op(",").Op("*").Id(g.className(c.IRI))).Block(
				Return().Id("typeIter").Params(Op("*").Id("o"), Id(castPrefix+g.className(c.IRI))),
			)
		}
	}
}

func (g *generator) pluralize(name string) string {
	return pluralize.NewClient().Plural(name)
}

func (g *generator) appendExternalIRI(f *File) {
	const structName = "ExternalIRI"

	// append type
	f.Type().Id(structName).Struct(
		Id(ld.GoIdField).Id("string").Tag(map[string]string{
			ld.GoIriTagName: ld.JsonIdProp,
		}),
		Id("value").Any(),
	)

	// append creation function
	f.Func().Id("New" + upperFirst(structName)).Params(Id("id").Id("string")).Op("*").Id(structName).Block(
		Return().Op("&").Id(structName).Block(
			Id(ld.GoIdField).Op(":").Id("id").Op(","),
		),
	)

	for _, name := range keys(g.nameToIRI) {
		iri := g.nameToIRI[name]
		if g.isEnum(iri) {
			continue
		}
		f.Func().Params(Id("o").Op("*").Id(structName)).Id(viewPrefix + name).Params().Op("*").Id(name).Block(
			Return().Id(castPrefix + name).Params(Id("o").Dot("value")),
		)
	}
}

func (g *generator) appendCastFuncs(f *File) {
	// append individual cast functions for each non-enum type
	for _, name := range keys(g.nameToIRI) {
		iri := g.nameToIRI[name]
		if g.isEnum(iri) {
			continue
		}
		f.Func().Id(castPrefix+name).Params(Id("o").Any()).Op("*").Id(name).Block(
			If(Id("o").Op(",").Id("ok").Op(":=").Id("o").Op(".").Params(Id(interfacePrefix+name)).Op(";").Id("ok").Block(
				Return().Id("o").Op(".").Id(viewPrefix+name).Params(),
			)),
			Return().Nil(),
		)
	}

	// append a singular cast function
	f.Func().Id(castPrefix).Index(Id("T").Id("any")).Params(Id("value").Id("any")).Op("*").Id("T").BlockFunc(func(f *Group) {
		f.Var().Id("t").Id("T")
		f.Switch(Any().Params(Id("t")).Op(".").Params(Type())).BlockFunc(func(f *Group) {
			for _, name := range keys(g.nameToIRI) {
				iri := g.nameToIRI[name]
				if g.isEnum(iri) {
					continue
				}
				f.Case(Id(name)).Block(
					If(Id("v").Op(",").Id("ok").Op(":=").Any().Params(Id(castPrefix + name).Params(Id("value"))).Op(".").Params(Op("*").Id("T")).Op(";").Id("ok")).Block(
						Return(Id("v")),
					),
				)
			}
		})
		f.Panic(Lit("invalid type cast, unknown type: ").Op("+").Qual("reflect", "TypeOf").Params(Id("t")).Op(".").Id("String").Params())
	})

	// append "As" function
	f.Func().Id("As").Index(Id("T").Any().Op(",").Id("R").Any()).Params(Id("value").Any(), Id("fn").Func().Params(Id("v").Op("*").Id("T")).Id("R")).Id("R").Block(
		Id("v").Op(":=").Id(castPrefix).Index(Id("T")).Params(Id("value")),
		If(Id("v").Op("!=").Nil().Block(
			Return(Id("fn").Params(Id("v"))),
		)),
		Var().Id("r").Id("R"),
		Return(Id("r")),
	)
}

func (g *generator) interfaceName(iri string) string {
	return interfacePrefix + g.className(iri)
}

func (g *generator) appendUtils(f *File) {
	f.Id(`
func typeIter[T any, E any](values []E, cast func(any) *T) iter.Seq2[E,*T] {
	if values == nil {
		return func(yield func(E,*T) bool) {}
	}
	return func(yield func(E,*T) bool) {
		for _, value := range values {
			v := cast(value)
			if v != nil {
				if !yield(value, v) {
					return
				}
			}
		}
	}
}
`)
}

func (g *generator) isSubtypeOf(parent *Class, typ *Class) bool {
	if typ.ParentIRI != "" {
		if typ.ParentIRI == parent.IRI {
			return true
		}
		next := g.iriToType[typ.ParentIRI]
		return g.isSubtypeOf(parent, next)
	}
	return false
}

func (g *generator) appendValidations(f *File) {
	f.Id(`
import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

type ValidationError struct {
	Path []string
	Err error
}

func (v ValidationError) String() string {
	return strings.Join(v.Path, ".") + ": " + v.Err.Error()
}

func (v ValidationError) Error() string {
	return v.String()
}

func newValidationError(path []string, err error) ValidationError {
	return ValidationError{
		Path: path,
		Err: err,
	}
}

type validator interface {
	Validate(visited map[any]struct{}, path ...string) []ValidationError
}

func validateInValues(path []string, value string, valid ...string) []ValidationError {
	if slices.Contains(valid, value) {
		return nil
	}
	return []ValidationError{newValidationError(path, fmt.Errorf("invalid value: '%v', expected: %v", value, valid))}
}
`)
	for _, name := range keys(g.nameToIRI) {
		iri := g.nameToIRI[name]
		c := g.iriToType[iri]

		f.Func().Params(Id("o").Op("*").Id(g.className(iri))).Id("Validate").Params(Id("visited").Id("map[any]struct{}"), Id("path").Op("...").Op("string")).Index().Id("ValidationError").BlockFunc(func(f *Group) {
			for _, prop := range c.Properties {
				for _, validation := range prop.Validations {
					switch v := validation.(type) {
					case AllowedIRIValidation:
						f.Comment("AllowedIRI: " + string(v))
					case MinIntValidation:
						f.Comment("MinIntValidation: " + fmt.Sprintf("%v", v))
					case MatchPatternValidation:
						f.Comment("MatchPattern: " + string(v))
					}
				}
			}
			f.Return(Nil())
		})
	}
}

func (g *generator) appendNamedIndividualsForType(f *File, sortedNamed []*Individual, typeIRI string) {
	for _, ni := range sortedNamed {
		if ni.TypeIRI != typeIRI {
			continue
		}
		label := cleanText(ni.Label)
		if label == "" {
			parts := strings.Split(ni.IRI, "/")
			label = parts[len(parts)-1]
		}
		c := g.iriToType[typeIRI]
		typeName := g.className(typeIRI)
		varName := typeName + "_" + upperFirst(label)
		if ni.Comment != "" {
			f.Comment(prefixWith(ni.Comment, varName))
		}
		f.Var().Id(varName).Op("=").Id(typeName).Block(
			g.setId(c, ni.IRI),
		)
	}
}

func (g *generator) setId(c *Class, iri string) Code {
	if c.ParentIRI != "" {
		parent := g.iriToType[c.ParentIRI]
		typeName := g.className(parent.IRI)
		return Id(typeName).Op(":").Id(typeName).Block(
			g.setId(parent, iri),
		).Op(",")
	}
	return Id(ld.GoIdField).Op(":").Lit(iri).Op(",")
}

func (g *generator) appendContextRegistration(f *File) {
	contextCreateName := "LDContext"
	if renamed := g.renameFunc(NameTypeFunc, contextCreateName); renamed != "" {
		contextCreateName = renamed
	}
	f.Func().Id(contextCreateName).Params().Qual(ldImport, "Context").BlockFunc(func(f *Group) {
		val := Return().Qual(ldImport, "Context").Block()
		//f.Id("o").Op(":=").Qual(ldImport, "Context").Block()
		for contextURI, contextJSON := range g.contexts {
			params := []Code{
				Lit(contextURI),
				getMap(contextJSON),
			}
			for _, name := range keys(g.nameToIRI) {
				iri := g.nameToIRI[name]
				typ := g.iriToType[iri]
				params = append(params, Id(typ.GoName).Block())
			}
			//f.Id("o").Dot("Register").Params(params...)
			val.Dot("Register").Params(params...)
		}
		//f.Return().Id("o")
		f.Add(val)
	})
}

func getMap(contextJSON map[string]any) Code {
	values := Dict{}
	for k, v := range contextJSON {
		switch v := v.(type) {
		case map[string]any:
			values[Lit(k)] = getMap(v)
		case []any:
			panic("unsupported list in context")
		default:
			values[Lit(k)] = Lit(v)
		}
	}
	return Map(String()).Any().Values(values)
}

func upperFirst(part string) string {
	return strings.ToUpper(part[:1]) + part[1:]
}

var requireMultipleSegments = os.Getenv("REQUIRE_MULTIPLE_SEGMENTS") == "true"
var interfacePrefix = "Any"
var listSuffix = "List"
var viewPrefix = "as"
var castPrefix = "cast"
var iterSuffix = "Iter"
