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

// Generate is responsible for taking a number of options and generating a data model,
// along with supporting code to stdout or a set of files. By default,
// the code will use the `model` package, and be output to stdout
func Generate(opts ...Option) {
	g := &generator{
		pkgName:           "model",
		license:           "UNKNOWN",
		outputValidations: true,
		classes:           map[string]*Class{},
		contexts:          map[string]map[string]any{},
		namedIndividuals:  map[string][]*Individual{},
		nameToIRI:         map[string]string{},
		iriToType:         map[string]*Class{},
		pluralizer:        pluralize.NewClient(),
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

func LicenseID(spdxLicenseID string) Option {
	return func(generator *generator) {
		generator.license = spdxLicenseID
	}
}

func UseEnums(useEnums bool) Option {
	return func(generator *generator) {
		generator.useEnums = useEnums
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
		for _, ni := range namedIndividuals {
			generator.namedIndividuals[ni.TypeIRI] = append(generator.namedIndividuals[ni.TypeIRI], ni)
		}
	}
}

type NameType int

const (
	NameTypeType NameType = iota
	NameTypeField
	NameTypeFunc
	NameTypeFile
	NameTypeComment
)

type renameFunc func(typ NameType, name string) string

type generator struct {
	pkgName           string
	license           string
	contexts          map[string]map[string]any
	classes           map[string]*Class        // classes by IRI
	namedIndividuals  map[string][]*Individual // individuals by IRI, sorted
	outputFile        string
	outputValidations bool
	nameToIRI         map[string]string
	iriToType         map[string]*Class
	pluralizer        *pluralize.Client
	renameFunc        renameFunc
	useEnums          bool
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

	for iri := range g.namedIndividuals {
		slices.SortFunc(g.namedIndividuals[iri], func(a, b *Individual) int {
			return strings.Compare(a.IRI, b.IRI)
		})
	}

	log("SUMMARY -- classes:", totalTypes, ", properties:", totalProps, ", individuals:", len(g.namedIndividuals))

	// sort output alphabetically by type name, the names have already been replaced
	for _, name := range keys(g.nameToIRI) {
		iri := g.nameToIRI[name]
		c := g.iriToType[iri]

		// append the interface for this struct, can be extended
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

		// implement the interface for this struct
		if !g.isEnum(c.IRI) {
			f.Func().Params(Id("o").Op("*").Id(name)).Id(viewPrefix + name).Params().Op("*").Id(name).Block(
				Return(Id("o")),
			)
		}

		// add all the named individuals defined for this type
		g.appendNamedIndividualsForType(f, c.IRI)

		// append the list type for this struct
		if g.isObject(c.IRI) && !g.isEnum(c.IRI) {
			g.appendListType(f, c)
		}
	}

	// append external IRI type
	g.appendExternalIRI(f)

	// append cast functions
	g.appendCastFuncs(f)

	// append context registration
	g.appendContextRegistration(f)

	log()

	commentText := []byte(g.name(NameTypeComment, fmt.Sprintf("// Generated by %s\n//\n// SPDX-License-Identifier: %s\n\n", ldImport, g.license)))
	if g.outputFile != "" {
		must(os.WriteFile(g.name(NameTypeFile, g.outputFile+".go"), append(commentText, formattedSource(f)...), 0777))
	}

	if g.outputValidations && g.outputFile != "" {
		f = g.newCode()
	}

	g.appendValidations(f)
	if g.outputFile != "" {
		must(os.WriteFile(g.name(NameTypeFile, g.outputFile+"_validations.go"), append(commentText, []byte(f.GoString())...), 0777))
	}

	if g.outputFile == "" {
		_, _ = os.Stdout.Write(append(commentText, f.GoString()...))
	}
}

func formattedSource(f *File) []byte {
	src := []byte(f.GoString())
	// split the ld.Context{}.Register() argument list
	parts := regexp.MustCompile(`}},`).Split(string(src), 2)
	parts[1] = regexp.MustCompile(`, `).ReplaceAllString(parts[1], ",\n")
	//parts[1] = regexp.MustCompile(`{}, `).ReplaceAllString(parts[1], "{},\n")
	parts[1] = regexp.MustCompile(`{}\)`).ReplaceAllString(parts[1], "{},\n)")
	src = get(format.Source([]byte(parts[0]+"}},"+parts[1]), format.Options{}))
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

	return g.name(NameTypeField, name)
}

func (g *generator) isEnum(typeIRI string) bool {
	if !g.useEnums {
		return false
	}
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
		idField := ld.GoIdField
		if g.isEnum(c.IRI) {
			idField = unexport(idField)
		}
		out = append(out, Id(idField).Id("string").Tag(map[string]string{
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
		tags := map[string]string{
			ld.GoIriTagName:  p.IRI,
			ld.GoTypeTagName: p.TypeIRI,
		}
		if p.MinCount > 0 {
			tags[ld.GoRequiredTagName] = "true"
		}
		out = append(out, Id(name).Add(g.fieldType(c, p)).Tag(tags))
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

// baseType returns the golang base type to output, with "primitive" values based on the type mappings defined in ld.TypeIRI2Go
func (g *generator) baseType(c *Class, p *Property) (pkg string, typ string) {
	iri := cleanIRI(p.TypeIRI)
	goTyp := ld.TypeForIRI(iri)
	if goTyp != nil {
		return goTyp.PkgPath(), goTyp.Name()
	}

	c = g.iriToType[iri]
	if c == nil {
		panic("Unknown type for IRI: " + iri)
	}
	name := g.name(NameTypeType, c.GoName)
	parts := strings.Split(name, ".")
	if len(parts) > 1 {
		return strings.Join(parts[0:len(parts)-1], "."), parts[len(parts)-1]
	}
	return "", name
}

func (g *generator) fieldName(_ *Class, p *Property) string {
	return g.name(NameTypeField, p.GoName)
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
	return g.name(NameTypeType, name)
}

func (g *generator) appendListType(f *File, c *Class) {
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
			getterName := g.name(NameTypeFunc, g.pluralize(g.className(c.IRI)))
			castName := g.name(NameTypeFunc, castPrefix+g.className(c.IRI))
			f.Func().Params(Id("o").Op("*").Id(listTypeName)).Id(getterName).Params().Qual(ldImport, "TypeSeq").Index(Id(g.interfaceName(listTyp.IRI)).Op(",").Op("*").Id(g.className(c.IRI))).Block(
				Return().Qual(ldImport, "NewTypeSeq").Params(Op("*").Id("o"), Id(castName)),
			)
		}
	}
}

func (g *generator) pluralize(name string) string {
	return g.pluralizer.Plural(name)
}

func (g *generator) appendExternalIRI(f *File) {
	structName := g.name(NameTypeType, externalIriName)

	// append type without type info, these will only output as an id
	f.Type().Id(structName).Struct(
		Id(unexport(ld.GoIdField)).Id("string").Tag(map[string]string{
			ld.GoIriTagName: ld.JsonIdProp,
		}),
		Id("value").Any(),
	)

	// append creation function
	f.Func().Id(g.externalIRIName()).Params(Id("id").Id("string")).Op("*").Id(structName).Block(
		Return().Op("&").Id(structName).Block(
			Id(unexport(ld.GoIdField)).Op(":").Id("id").Op(","),
		),
	)

	for _, name := range keys(g.nameToIRI) {
		iri := g.nameToIRI[name]
		if g.isEnum(iri) {
			continue
		}
		castName := g.name(NameTypeFunc, castPrefix+name)
		f.Func().Params(Id("o").Op("*").Id(structName)).Id(viewPrefix + name).Params().Op("*").Id(name).Block(
			Return().Id(castName).Params(Id("o").Dot("value")),
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
		castName := g.name(NameTypeFunc, castPrefix+name)
		f.Func().Id(castName).Params(Id("o").Any()).Op("*").Id(name).Block(
			If(Id("o").Op(",").Id("ok").Op(":=").Id("o").Op(".").Params(Id(interfacePrefix+name)).Op(";").Id("ok").Block(
				Return().Id("o").Op(".").Id(viewPrefix+name).Params(),
			)),
			Return().Nil(),
		)
	}

	castName := g.name(NameTypeFunc, castPrefix)
	// append a singular cast function
	f.Func().Id(castName).Index(Id("T").Id("any")).Params(Id("value").Id("any")).Op("*").Id("T").BlockFunc(func(f *Group) {
		f.Var().Id("t").Id("T")
		f.Switch(Any().Params(Id("t")).Op(".").Params(Type())).BlockFunc(func(f *Group) {
			for _, name := range keys(g.nameToIRI) {
				iri := g.nameToIRI[name]
				if g.isEnum(iri) {
					continue
				}
				castName = g.name(NameTypeFunc, castPrefix+name)
				f.Case(Id(name)).Block(
					If(Id("v").Op(",").Id("ok").Op(":=").Any().Params(Id(castName).Params(Id("value"))).Op(".").Params(Op("*").Id("T")).Op(";").Id("ok")).Block(
						Return(Id("v")),
					),
				)
			}
		})
		f.Panic(Lit("invalid type cast, unknown type: ").Op("+").Qual("reflect", "TypeOf").Params(Id("t")).Op(".").Id("String").Params())
	})

	castName = g.name(NameTypeFunc, castPrefix)
	// append "As" function
	f.Func().Id("As").Index(Id("T").Any().Op(",").Id("R").Any()).Params(Id("value").Any(), Id("fn").Func().Params(Id("v").Op("*").Id("T")).Id("R")).Id("R").Block(
		Id("v").Op(":=").Id(castName).Index(Id("T")).Params(Id("value")),
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

func (g *generator) appendNamedIndividualsForType(f *File, typeIRI string) {
	for _, ni := range g.namedIndividuals[typeIRI] {
		varName := g.namedIndividualName(ni)
		if ni.Comment != "" {
			f.Comment(prefixWith(ni.Comment, varName))
		}
		if g.useEnums && g.isEnum(typeIRI) {
			c := g.iriToType[typeIRI]
			typeName := g.className(typeIRI)
			f.Var().Id(varName).Op("=").Id(typeName).Block(
				g.setId(c, ni.IRI),
			)
		} else {
			f.Var().Id(varName).Id(g.interfaceName(typeIRI)).Op("=").Op("&").Id(externalIriName).Block(
				Id(unexport(ld.GoIdField)).Op(":").Lit(ni.IRI).Op(","),
			)
		}
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
	if g.isEnum(c.IRI) {
		return Id(unexport(ld.GoIdField)).Op(":").Lit(iri).Op(",")
	}
	return Id(ld.GoIdField).Op(":").Lit(iri).Op(",")
}

func (g *generator) appendContextRegistration(f *File) {
	contextCreateName := g.name(NameTypeFunc, "context")
	f.Func().Id(contextCreateName).Params().Qual(ldImport, "Context").BlockFunc(func(f *Group) {
		val := Return().Qual(ldImport, "NewContext").Params()
		for contextURI, contextJSON := range g.contexts {
			params := []Code{
				Lit(contextURI),
				getMap(contextJSON),
				Id(g.externalIRIName()),
			}
			for _, name := range keys(g.nameToIRI) {
				iri := g.nameToIRI[name]
				typ := g.iriToType[iri]
				params = append(params, Id(typ.GoName).Block())
				for _, ni := range g.namedIndividuals[iri] {
					params = append(params, Id(g.namedIndividualName(ni)))
				}
			}
			val.Dot("Register").Params(params...)
		}
		f.Add(val)
	})
}

func (g *generator) name(typ NameType, name string) string {
	renamed := g.renameFunc(typ, name)
	if renamed != "" {
		return renamed
	}
	return name
}

func (g *generator) namedIndividualName(ni *Individual) string {
	label := cleanText(ni.Label)
	if label == "" {
		parts := strings.Split(ni.IRI, "/")
		label = parts[len(parts)-1]
	}
	label = g.name(NameTypeField, label)
	typeName := g.className(ni.TypeIRI)
	return typeName + "_" + upperFirst(label)
}

func (g *generator) externalIRIName() string {
	structName := g.name(NameTypeType, externalIriName)
	funcName := g.name(NameTypeFunc, "New"+structName)
	funcName = upperFirst(funcName)
	return funcName
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

func unexport(part string) string {
	return strings.ToLower(part)
}

var requireMultipleSegments = os.Getenv("REQUIRE_MULTIPLE_SEGMENTS") == "true"
var interfacePrefix = "Any"
var listSuffix = "List"
var viewPrefix = "as"
var castPrefix = "cast"
var ldImport = reflect.TypeOf(ld.Type{}).PkgPath()
var externalIriName = "ExternalIRI"
