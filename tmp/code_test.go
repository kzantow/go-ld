package v3_0_1_test

import (
	"fmt"
	spdx "github.com/kzantow/go-ld/tmp"
	"testing"
)

func Test_code(t *testing.T) {
	p := &spdx.Package{
		SoftwareArtifact: spdx.SoftwareArtifact{
			Artifact: spdx.Artifact{
				Element: spdx.Element{
					ID:   "whee",
					Name: "a-pkg",
				},
			},
		},
		PackageVersion: "1",
	}

	doc := &spdx.SpdxDocument{}
	doc.Elements = append(doc.Elements, p)

	for _, p2 := range doc.Elements.PackageIter() {
		fmt.Println("Pkg: " + p2.Name)
	}
}
