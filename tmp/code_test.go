package v3_0_test

import (
	"fmt"
	"testing"

	spdx "github.com/kzantow/go-ld/tmp"
)

func Test_equality(t *testing.T) {
	o := spdx.Organization{Agent: spdx.Agent{Element: spdx.Element{ID: spdx.Organization_SpdxOrganization.ID}}}
	if spdx.Organization_SpdxOrganization.ID != o.ID {
		t.Fatal("not equal")
	}
}

func Test_code(t *testing.T) {
	p := &spdx.Package{
		SoftwareArtifact: spdx.SoftwareArtifact{
			PrimaryPurpose: spdx.SoftwarePurpose_Library,
			AdditionalPurposes: []spdx.SoftwarePurpose{
				spdx.SoftwarePurpose_Data,
				spdx.SoftwarePurpose_Container,
			},
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

	for o, p2 := range doc.Elements.PackageIter() {
		fmt.Println("Pkg: " + p2.Name)

		asName := spdx.As(o, func(v *spdx.Package) string {
			return v.Name
		})

		fmt.Println("AsPkg.Name: " + asName)

		_ = spdx.As(o, func(v *spdx.SoftwareArtifact) error {
			fmt.Println("Primary Purpose: " + v.PrimaryPurpose.ID)
			for _, add := range v.AdditionalPurposes {
				fmt.Println("Additional Purpose: " + add.ID)
			}
			return nil
		})
	}
}
