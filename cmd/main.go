package main

import (
	"strings"

	"github.com/kzantow/go-ld/gosh"
)

func main() {
	gosh.Generate(
		//gosh.EnableLog(),
		gosh.PackageName("v3_0"),
		gosh.OutputFile("tmp/model.go"),
		gosh.RenameFunc(renameFunc),
		gosh.JsonLDContext("https://spdx.github.io/spdx-spec/v3.0.1/rdf/spdx-context.jsonld"),
		gosh.SHACLTypes("https://spdx.github.io/spdx-spec/v3.0.1/rdf/spdx-model.ttl"),
	)
}

func renameFunc(typ gosh.NameType, name string) string {
	if typ == gosh.NameTypeField {
		return replaceSuffixes(name, map[string]string{
			"Bies": "By",
			"Tos":  "To",
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
