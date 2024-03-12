/*
Copyright 2022 The Flux authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

// import (
// 	"testing"

// 	fuzz "github.com/AdaLogics/go-fuzz-headers"

// 	"github.com/fluxcd/image-automation-controller/internal/source"
// )

// func Fuzz_templateMsg(f *testing.F) {
// 	f.Add("template", []byte{})
// 	f.Add("", []byte{})

// 	f.Fuzz(func(t *testing.T, template string, seed []byte) {
// 		var values source.TemplateData
// 		fuzz.NewConsumer(seed).GenerateStruct(&values)

// 		_, _ = templateMsg(template, &values)
// 	})
// }
