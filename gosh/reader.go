package gosh

import (
	"bytes"
	"slices"
	"strconv"
	"strings"

	"github.com/deiu/rdf2go"
)

func ParseSHACL(url string) *Context {
	spdxDefinitions := fetch(url)

	g := rdf2go.NewGraph(url)
	must(g.Parse(bytes.NewReader(spdxDefinitions), "text/turtle"))

	ctx := NewContext(cleanIRI(url))

	var allUsedProps []*rdf2go.Triple
	used := func(triple ...*rdf2go.Triple) {
		allUsedProps = append(allUsedProps, triple...)
	}

	classes := g.All(nil, rdfType, owlClass)
	for _, class := range sorted(classes, bySubject) {
		log("Class", class)

		out := &Class{
			IRI:     cleanIRI(class.Subject.String()),
			Comment: getComment(g, class.Subject, used),
		}
		if _, ok := ctx.Classes[out.IRI]; ok {
			panic("duplicate type definition: " + out.IRI)
		}
		ctx.Classes[out.IRI] = out

		superclass := oneOptional(g.All(class.Subject, rdfSubclassOf, nil))
		used(superclass)
		if superclass != nil {
			out.ParentIRI = cleanIRI(superclass.Object.String())
			log("  extends: ", out.ParentIRI)
		}

		// TODO what does this mean?: Predicate: <http://www.w3.org/ns/shacl#nodeKind>, Object: <http://www.w3.org/ns/shacl#IRI>

		properties := g.All(class.Subject, shaclProperty, nil)
		used(properties...)

		for _, property := range sorted(properties, byObject) {
			path := oneRequired(g.All(property.Object, shaclPath, nil))
			log("  property:", class.Subject.String(), "path", path.Object.String())

			if path.Object.Equal(rdfType) {
				log("  ABSTRACT TYPE MARKER!?")
				continue
			}

			prop := &Property{
				IRI:     cleanIRI(path.Object.String()),
				Comment: getComment(g, path.Object, used),
			}
			out.Properties = append(out.Properties, prop)

			// get the data type
			typeIRI := oneOptional(g.All(property.Object, shaclClass, nil))
			if typeIRI == nil {
				typeIRI = oneOptional(g.All(property.Object, shaclDatatype, nil))
			}
			used(typeIRI)
			if typeIRI != nil {
				prop.TypeIRI = cleanIRI(typeIRI.Object.String())
			} else {
				panic("No type IRI for: " + property.Object.String())
			}

			minCount := oneOptional(g.All(property.Object, shaclMinCount, nil))
			used(minCount)
			if minCount != nil {
				prop.Min = parseIntegerValue(minCount)
			}

			maxCount := oneOptional(g.All(property.Object, shaclMaxCount, nil))
			used(maxCount)
			if maxCount != nil {
				prop.Max = parseIntegerValue(maxCount)
			}

			allowedValues := oneOptional(g.All(property.Object, shaclIn, nil))
			var usedPropertyNodes []*rdf2go.Triple
			for allowedValues != nil {
				usedPropertyNodes = append(usedPropertyNodes, allowedValues)
				validation := oneOptional(g.All(allowedValues.Object, rdfFirst, nil))
				if validation != nil {
					log("    validation:", nodeDisplay(validation))
					prop.AllowedIRIs = append(prop.AllowedIRIs, cleanIRI(validation.Object.String()))
				}
				allowedValues = oneOptional(g.All(allowedValues.Object, rdfRest, nil))
			}

			allProps := g.All(property.Object, nil, nil)
			for _, p := range sorted(allProps, byObject) {
				if slices.Contains(append(usedPropertyNodes, path, property), p) {
					continue
				}
				log("  ... extra prop prop:", p)
			}
		}

		allprops := g.All(class.Subject, nil, nil)
		for _, p := range sorted(allprops, byObject) {
			if slices.Contains(allUsedProps, p) {
				continue
			}
			log("  ... unused class prop:", p)
		}
	}

	log("------------------- UNUSED PROPS -------------------")
	for p := range g.IterTriples() {
		if slices.Contains(allUsedProps, p) {
			continue
		}
		log("  ... unused class prop:", p)
	}

	return ctx
}

func NewContext(url string) *Context {
	return &Context{
		IRI:     url,
		Classes: nameMap{},
	}
}

func getComment(g *rdf2go.Graph, subject rdf2go.Term, used func(triple ...*rdf2go.Triple)) string {
	allComments := g.All(subject, rdfComment, nil)
	var comment *rdf2go.Triple
	for _, c := range allComments {
		value := c.Object.String()
		comment = c
		if strings.HasSuffix(value, "@en") {
			// use English comment
			break
		}
	}
	if comment != nil {
		used(comment)
		value := comment.Object.String()
		parts := strings.Split(value, "@")
		value = strings.Join(parts[:len(parts)-1], "@")
		return strings.TrimSpace(strings.Trim(value, "\""))
	}
	return ""
}

func parseIntegerValue(count *rdf2go.Triple) int {
	if count == nil {
		return 0
	}
	val := count.Object.String()
	val = strings.Split(val, "^")[0]
	val = strings.Trim(val, "\"")
	return get(strconv.Atoi(val))
}

var (
	rdfType       = rdf2go.NewResource("http://www.w3.org/1999/02/22-rdf-syntax-ns#type")
	rdfFirst      = rdf2go.NewResource("http://www.w3.org/1999/02/22-rdf-syntax-ns#first")
	rdfRest       = rdf2go.NewResource("http://www.w3.org/1999/02/22-rdf-syntax-ns#rest")
	rdfSubclassOf = rdf2go.NewResource("http://www.w3.org/2000/01/rdf-schema#subClassOf")
	rdfComment    = rdf2go.NewResource("http://www.w3.org/2000/01/rdf-schema#comment")
	owlClass      = rdf2go.NewResource("http://www.w3.org/2002/07/owl#Class")
	//owlObjectProperty = rdf2go.NewResource("http://www.w3.org/2002/07/owl#ObjectProperty")
	//shaclNodeShape    = rdf2go.NewResource("http://www.w3.org/ns/shacl#NodeShape")
	//shaclNodeKind     = rdf2go.NewResource("http://www.w3.org/ns/shacl#nodeKind")
	shaclProperty = rdf2go.NewResource("http://www.w3.org/ns/shacl#property")
	shaclClass    = rdf2go.NewResource("http://www.w3.org/ns/shacl#class")
	shaclPath     = rdf2go.NewResource("http://www.w3.org/ns/shacl#path")
	shaclDatatype = rdf2go.NewResource("http://www.w3.org/ns/shacl#datatype")
	shaclIn       = rdf2go.NewResource("http://www.w3.org/ns/shacl#in")
	shaclMinCount = rdf2go.NewResource("http://www.w3.org/ns/shacl#minCount")
	shaclMaxCount = rdf2go.NewResource("http://www.w3.org/ns/shacl#maxCount")
)
