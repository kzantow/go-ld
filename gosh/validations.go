package gosh

import (
	"reflect"
	"slices"
	"strings"
	"time"

	. "github.com/dave/jennifer/jen"

	"github.com/kzantow/go-ld"
)

func (g *generator) appendValidations(f *File) {
	// these types do not need further pattern validation or otherwise implement their own Validate function
	validatingTypes := []reflect.Type{
		reflect.TypeOf(ld.URI("")),
		reflect.TypeOf(time.Time{}),
		reflect.TypeOf(ld.DateTime{}),
		reflect.TypeOf(ld.PositiveInt(0)),
		reflect.TypeOf(ld.NonNegativeInt(0)),
	}
	for _, name := range keys(g.nameToIRI) {
		iri := g.nameToIRI[name]
		c := g.iriToType[iri]

		validationFunc := Func().Params(Id("o").Op("*").Id(g.className(iri))).Id("Validate").Params().Id("error").Block(
			Return(Qual(ldImport, "JoinErrors").ParamsFunc(func(f *Group) {
				// 		ld.ValidateProp(o, &o.DatasetTypes,
				//			ld.ValidateSlice(ld.ValidateID[DatasetType](ld.ValidateValues[string](
				//				"https://spdx.org/rdf/3.0.1/terms/Software/ContentIdentifierType/gitoid",
				//				"https://spdx.org/rdf/3.0.1/terms/Software/ContentIdentifierType/swhid",
				//			))),
				//		),

				if c.ParentIRI != "" {
					f.Line().Qual(ldImport, "ValidateProp").Params(Id("o"), Op("&").Id("o").Dot(g.className(c.ParentIRI)))
				}
				for _, p := range c.Properties {
					fieldType := g.fieldType(c, p)

					validatePropParams := []Code{Id("o"), Op("&").Id("o").Dot(g.propName(c, p))}

					// some validations are unnecessary such as time.Time, since we store a time representation instead of arbitrary string
					skipTypeValidations := slices.Contains(validatingTypes, ld.TypeForIRI(p.TypeIRI))
					if g.isList(c, p) && p.MinCount > 0 {
						validatePropParams = append(validatePropParams, Line().Qual(ldImport, "ValidateMinCount").Index(fieldType).Params(Lit(p.MinCount)))
					}

					if g.isList(c, p) && p.MaxCount > 1 {
						validatePropParams = append(validatePropParams, Line().Qual(ldImport, "ValidateMaxCount").Index(fieldType).Params(Lit(p.MaxCount)))
					}

					var allowedIRIs []string
					for _, validation := range p.Validations {
						switch v := validation.(type) {
						case AllowedIRIValidation:
							allowedIRIs = append(allowedIRIs, string(v))
						case MatchPatternValidation:
							if skipTypeValidations {
								continue
							}
							expr := strings.ReplaceAll(string(v), "\\\\", "\\")
							validatePropParams = append(validatePropParams, Line().Qual(ldImport, "ValidateExpression").Params(Lit(expr)))
						}
					}

					if len(allowedIRIs) > 0 {
						var validateValuesParams []Code
						for _, allowedIRI := range allowedIRIs {
							validateValuesParams = append(validateValuesParams, Line().Lit(cleanIRI(allowedIRI)))
						}
						idCheck := Qual(ldImport, "ValidateID").Index(Id(g.className(p.TypeIRI))).Params(
							Qual(ldImport, "ValidateValues").Params(append(validateValuesParams, Line())...),
						)
						if g.isList(c, p) {
							idCheck = Qual(ldImport, "ValidateSlice").Params(idCheck)
						}
						validatePropParams = append(validatePropParams, Line().Add(idCheck))
					}

					// first 2 params are initialized as object, property --
					// only append a property validation if we added any validations or if the property is required
					if len(validatePropParams) > 2 || p.MinCount > 0 {
						f.Line().Qual(ldImport, "ValidateProp").Params(validatePropParams...)
					}
				}
			})),
		)
		f.Add(validationFunc)
	}
}
