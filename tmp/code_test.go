package v3_0_test

import (
	"fmt"
	"testing"

	spdx "github.com/kzantow/go-ld/tmp"
)

func Test_equality(t *testing.T) {
	o := spdx.Organization_SpdxOrganization
	if o != spdx.Organization_SpdxOrganization {
		t.Fatal("not equal")
	}
}

func Test_code(t *testing.T) {
	p := &spdx.Package{
		SoftwareArtifact: spdx.SoftwareArtifact{
			Artifact: spdx.Artifact{Element: spdx.Element{
				ID:   "whee",
				Name: "a-pkg",
			}},
			PrimaryPurpose: spdx.SoftwarePurpose_Library,
			AdditionalPurposes: spdx.SoftwarePurposeList{
				spdx.SoftwarePurpose_Data,
				spdx.SoftwarePurpose_Container,
			},
		},
		PackageVersion: "1",
	}

	doc := &spdx.SpdxDocument{}
	doc.Elements = append(doc.Elements, p)

	for o, p2 := range doc.Elements.PackageIter() {
		fmt.Println("Pkg: " + p2.Name)

		asName := spdx.As(o, func(v *spdx.Package) string {
			return v.Name
		})

		fmt.Println("AsPkg.Name: " + asName)

		_ = spdx.As(o, func(v *spdx.SoftwareArtifact) error {
			fmt.Printf("Primary Purpose: %v\n", v.PrimaryPurpose)
			for _, add := range v.AdditionalPurposes {
				fmt.Printf("Additional Purpose: %v\n", add)
			}
			return nil
		})
	}
}
