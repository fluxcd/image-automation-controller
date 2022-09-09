package controllers

import (
	"testing"

	fuzz "github.com/AdaLogics/go-fuzz-headers"
)

func Fuzz_templateMsg(f *testing.F) {
	f.Add("template", []byte{})
	f.Add("", []byte{})

	f.Fuzz(func(t *testing.T, template string, seed []byte) {
		var values TemplateData
		fuzz.NewConsumer(seed).GenerateStruct(&values)

		_, _ = templateMsg(template, &values)
	})
}
