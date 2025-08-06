package main

import (
	"strings"

	"github.com/kzantow/go-ld/gosh"
)

func main() {
	gosh.Generate(
		//gosh.EnableLog(),
		gosh.PackageName("v3_0"),
		gosh.LicenseID("MIT"),
		gosh.OutputFile("tmp/model.go"),
		gosh.RenameFunc(renameFunc),
		gosh.JsonLDContext("https://spdx.org/rdf/3.0.1/spdx-context.jsonld"),
		gosh.SHACLTypes("https://spdx.org/rdf/3.0.1/spdx-model.ttl"),
	)
}

func renameFunc(typ gosh.NameType, name string) string {
	if typ == gosh.NameTypeField {
		return replaceSuffixes(name, map[string]string{
			"Bies":           "By",
			"Tos":            "To",
			"CreatedUsings":  "CreatedUsing",
			"VerifiedUsings": "VerifiedUsing",
		})
	}
	switch name {
	case "AnyLicenseInfo":
		return "LicenseInfo"
	}
	return ""
}

func replaceSuffixes(value string, suffixToReplacement map[string]string) string {
	for suffix, replacement := range suffixToReplacement {
		if strings.HasSuffix(value, suffix) {
			value = strings.TrimSuffix(value, suffix)
			return value + replacement
		}
	}
	return ""
}
