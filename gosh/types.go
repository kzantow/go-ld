package gosh

type Context struct {
	IRI              string
	Classes          map[string]*Class // classes by IRI
	NamedIndividuals []*Individual
}

func (c Context) Class(iri string) *Class {
	return c.Classes[iri]
}

type Individual struct {
	IRI     string
	TypeIRI string
	Label   string
	Comment string
}

type Class struct {
	IRI        string
	GoName     string
	Comment    string
	ParentIRI  string
	Properties []*Property
}

type Property struct {
	IRI         string
	GoName      string
	Comment     string
	TypeIRI     string
	MinCount    int
	MaxCount    int
	Validations []any
}

type AllowedIRIValidation string

type MinIntValidation int

type MatchPatternValidation string
