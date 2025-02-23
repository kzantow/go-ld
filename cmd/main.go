package main

import (
	"strings"

	"github.com/kzantow/go-ld/gosh"
)

func main() {
	ctx := gosh.ParseSHACL(
		"https://spdx.github.io/spdx-spec/v3.0/rdf/spdx-model.ttl",
	)

	// "https://spdx.github.io/spdx-spec/v3.0.1/rdf/spdx-context.jsonld"
	g := gosh.NewGenerator(gosh.RenameFunc(renamer))
	g.Generate(ctx, "v3_0_1", "tmp/code.go")
}

func renamer(typ gosh.NameType, name string) string {
	if typ == gosh.NameTypeField {
		return dePluralizeSuffixes(name, "By", "To")
	}
	switch name {
	case "AnyLicenseInfo":
		return "LicenseInfo"
	}
	return ""
}

func dePluralizeSuffixes(value string, names ...string) string {
	for _, suffix := range names {
		bad := suffix + "s"
		if strings.HasSuffix(value, bad) {
			value = strings.TrimSuffix(value, bad)
			return value + suffix
		}
	}
	return ""
}
