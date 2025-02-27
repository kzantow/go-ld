package gosh

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
