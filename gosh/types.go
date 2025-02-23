package gosh

type Context struct {
	IRI     string
	Classes map[string]*Class // classes by IRI
}

func (c Context) Class(iri string) *Class {
	return c.Classes[iri]
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
	Min         int
	Max         int
	AllowedIRIs []string
}
