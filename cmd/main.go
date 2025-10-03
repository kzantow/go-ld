package main

import (
	"github.com/kzantow/go-ld/shaclgen"
)

func main() {
	shaclgen.Generate(
		shaclgen.EnableLog(),
		shaclgen.PackageName("v3_0_1"),
		shaclgen.LicenseID("MIT"),
		shaclgen.OutputFile("tmp/model.go"),
		shaclgen.JsonLDContext("https://spdx.org/rdf/3.0.1/spdx-context.jsonld"),
		shaclgen.SHACLTypes("https://spdx.org/rdf/3.0.1/spdx-model.ttl"),
	)
}
