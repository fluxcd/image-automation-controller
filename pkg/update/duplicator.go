/*
Copyright 2020, 2021 The Flux authors

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

package update

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	imagev1_reflect "github.com/fluxcd/image-reflector-controller/api/v1beta2"
	"github.com/go-logr/logr"
	"sigs.k8s.io/kustomize/kyaml/kio"
	"sigs.k8s.io/kustomize/kyaml/kio/kioutil"
	"sigs.k8s.io/kustomize/kyaml/sets"
	"sigs.k8s.io/kustomize/kyaml/yaml"
	"strings"
)

// UpdateWithDuplicator takes all YAML files from `inpath`, updates/create any
// that contain an "in scope" image policy marker, and writes files it
// updated (and only those files) back to `outpath`.
func UpdateWithDuplicator(tracelog logr.Logger, inpath, outpath string, policies []imagev1_reflect.ImagePolicy) (Result, error) {
	result := Result{
		Files: make(map[string]FileResult),
	}

	// Compiling the result needs the file, the image ref used, and the
	// object. Each setter will supply its own name to its callback,
	// which can be used to look up the image ref; the file and object
	// we will get from `setAll` which keeps track of those as it
	// iterates.
	imageRefs := make(map[string]imageRef)

	setAllCallback := func(file, setterName string, node *yaml.RNode) {
		ref, ok := imageRefs[setterName]
		if !ok {
			return
		}

		meta, err := node.GetMeta()
		if err != nil {
			return
		}
		oid := ObjectIdentifier{meta.GetIdentifier()}

		fileres, ok := result.Files[file]
		if !ok {
			fileres = FileResult{
				Objects: make(map[ObjectIdentifier][]ImageRef),
			}
			result.Files[file] = fileres
		}
		objres, ok := fileres.Objects[oid]
		for _, n := range objres {
			if n == ref {
				return
			}
		}
		objres = append(objres, ref)
		fileres.Objects[oid] = objres
	}

	// get ready with the reader and writer
	reader := &ScreeningLocalReader{
		Path:  inpath,
		Token: fmt.Sprintf("%q", SetterShortHand),
		Trace: tracelog,
	}
	writer := &kio.LocalPackageWriter{
		PackagePath: outpath,
	}

	policiesMap := map[string]imagev1_reflect.ImagePolicy{}

	for _, policy := range policies {
		policiesMap[fmt.Sprintf("%s:%s", policy.GetNamespace(), policy.GetName())] = policy
	}

	worker := &duplicatorWorker{}
	worker.policies = policiesMap
	worker.inpath = inpath
	worker.tracelog = tracelog
	worker.fileChanges = make(map[string][]duplicatorObject)


	pipeline := kio.Pipeline{
		Inputs:  []kio.Reader{reader},
		Outputs: []kio.Writer{writer},
		Filters: []kio.Filter{
			//setAll(&settersSchema, tracelog, setAllCallback),
			simpleFilter(worker, setAllCallback),
		},
	}

	// go!
	err := pipeline.Execute()
	if err != nil {
		return Result{}, err
	}
	return result, nil

}

type duplicatorObject struct {
	object *yaml.RNode
	changes []duplicatorNode
}

type duplicatorNode struct {
	node *yaml.RNode
	parameter map[string]string
}

type duplicatorWorker struct {
	tracelog logr.Logger
	inpath string
	policies map[string]imagev1_reflect.ImagePolicy
	fileChanges map[string][]duplicatorObject
}

func simpleFilter(worker *duplicatorWorker, resultCallback func(file string, setterName string, node *yaml.RNode)) kio.Filter {
	return kio.FilterFunc(
		func(nodes []*yaml.RNode) ([]*yaml.RNode, error) {
			filesToUpdate, err := worker.fillFileChanges(nodes)
			if err != nil {
				return nil, err
			}

			if err = worker.updateLatest(); err!=nil {
				return nil, err
			}

			var nodesInUpdatedFiles []*yaml.RNode
			for i := range nodes {
				path, _, err := kioutil.GetFileAnnotations(nodes[i])
				if err != nil {
					return nil, err
				}
				if filesToUpdate.Has(path) {
					nodesInUpdatedFiles = append(nodesInUpdatedFiles, nodes[i])
				}
			}
			return nodesInUpdatedFiles, nil
		})
}

func (worker *duplicatorWorker) fillFileChanges(nodes []*yaml.RNode) (sets.String, error) {
	filesToUpdate := sets.String{}
	nbObjectToChange := 0
	nbNodeToChange := 0

	for i := range nodes {
		path, _, err := kioutil.GetFileAnnotations(nodes[i])
		if err != nil {
			return nil, err
		}

		listChange := []duplicatorNode{}

		if err := worker.findNodeWithPolicies(nodes[i], "", &listChange); err != nil {
			return nil, err
		}

		if len(listChange) > 0 {
			filesToUpdate.Insert(path)
			nbObjectToChange++
			nbNodeToChange += len(listChange)

			changeObject := duplicatorObject{
				object:  nodes[i],
				changes: listChange,
			}

			if _, ok := worker.fileChanges[path]; !ok {
				worker.fileChanges[path] = []duplicatorObject{
					changeObject,
				}
			} else {
				worker.fileChanges[path] = append(worker.fileChanges[path], changeObject)
			}
		}
	}

	worker.tracelog.Info("Found", "nb node to change", nbNodeToChange, "nb object to change", nbObjectToChange, "file to change", len(filesToUpdate))

	return filesToUpdate, nil
}

func (w *duplicatorWorker) findNodeWithPolicies(object *yaml.RNode, path string, toModify *[]duplicatorNode) error{
	// accept walks the AST and calls the visitor at each scalar node.
	switch object.YNode().Kind {
	case yaml.DocumentNode:
		// Traverse the child of the document
		return w.findNodeWithPolicies(yaml.NewRNode(object.YNode()), path, toModify)
	case yaml.MappingNode:
		return object.VisitFields(func(node *yaml.MapNode) error {
			// Traverse each field value
			return w.findNodeWithPolicies(node.Value, path+"."+node.Key.YNode().Value, toModify)
		})
	case yaml.SequenceNode:
		return object.VisitElements(func(node *yaml.RNode) error {
			// Traverse each list element
			return w.findNodeWithPolicies(node, path, toModify)
		})
	case yaml.ScalarNode:
		return w.detectScalarWithPolicy(object, path, toModify)
	}
	return nil
}

func (w *duplicatorWorker) detectScalarWithPolicy(node *yaml.RNode, path string, toModify *[]duplicatorNode) error {
	comment := node.YNode().LineComment

	if comment == "" {
			return nil
		}

	comment = strings.TrimLeft(comment, "#")

	input := map[string]string{}
	err := json.Unmarshal([]byte(comment), &input)
	if err != nil {
		return nil
	}
	name := input[SetterShortHand]
	if name == "" {
		return nil
	}
	split := strings.Split(name, ":")
	if len(split) < 2 || len(split) > 3 {
		return nil

	}

	if _, ok := w.policies[fmt.Sprintf("%s:%s",split[0],split[1])] ; ok {
		w.tracelog.Info("Found parametrized node", "path", path)
		ref := duplicatorNode{
			node: node,
			parameter: input,
		}
		*toModify = append(*toModify, ref)
	}

	return nil
}


func (worker *duplicatorWorker) updateLatest() error {
	for path, lstObject := range worker.fileChanges {
		var filePolicy *imagev1_reflect.ImagePolicy
		for _, object := range lstObject {
			for _, change := range object.changes {
				p, err := worker.updateNode(change.node, change.parameter, true)
				if err!=nil {
					return err
				}
				if filePolicy == nil {
					filePolicy = p
				} else {
					if filePolicy != filePolicy {
						return fmt.Errorf("Policy name mismatch for file %s", path)
					}
				}
			}
		}
		if filePolicy == nil {
			continue
		}
		existingDiscriminator := worker.existingDiscriminor(path)

		// Delete the old discriminators file
		for _, d := range existingDiscriminator {
			if _, ok := filePolicy.Status.Distribution[d]; !ok {
				_, _, fd := buildFilename(filepath.Join(worker.inpath, path), d)
				if os.Remove(fd) != nil {
					return fmt.Errorf("Unable to remove file %s", fd)
				}
			}
		}
		// Update the existing discriminator file if needed
		for _, d := range existingDiscriminator {
			if _, ok := filePolicy.Status.Distribution[d]; ok {
				_, _, fd := buildFilename(filepath.Join(worker.inpath, path), d)
				nodes, err := kioReadFile(worker.inpath, fd)
				if err!=nil {
					return err
				}

			}
		}
		// Create the new discriminator file
	}
	return nil
}

func (worker *duplicatorWorker) updateNode(node *yaml.RNode, parameter map[string]string, keepComment bool) (*imagev1_reflect.ImagePolicy, error) {
	policyFull := parameter[SetterShortHand]
	policySplitted := strings.Split(policyFull, ":")
	policyStr := fmt.Sprintf("%s:%s", policySplitted[0], policySplitted[1])
	additionalTag := ""
	if (len(policySplitted) == 3) {
		additionalTag = policySplitted[2]
	}
	policy := worker.policies[policyStr]

	if policy.Status.LatestImage == "" {
		return &policy, nil
	}

	tmpl := ""
	if t, ok := parameter["template"]; ok {
		tmpl = t
	}
	if additionalTag != "" {
		switch additionalTag {
		case "tag":
			tmpl = "{{.Tag}}"
		case "name":
			tmpl = "{{.Image}}"
		}
	}
	if tmpl == "" {
		tmpl = "{{.Image}}:{{.Tag}}"
	}

	data := policy.Status.Distribution[policy.Status.LatestDiscriminator]

	t := template.Must(template.New("").Parse(tmpl))
	builder := &strings.Builder{}
	if err := t.Execute(builder, data); err != nil {
		return nil, err
	}

	node.YNode().Value = builder.String()
	if !keepComment {
		node.YNode().LineComment = ""
	}

	return &policy, nil
}

func (worker *duplicatorWorker) existingDiscriminor(path string) []string {
	filename, ext, glob := buildFilename(filepath.Join(worker.inpath, path), "*")
	lstFile, err := filepath.Glob(glob)
	if lstFile == nil || err != nil {
		return []string{}
	}
	for i := range lstFile {
		_, lstFile[i] = filepath.Split(lstFile[i])
		lstFile[i] = strings.TrimPrefix(lstFile[i], filename+"__")
		lstFile[i] = strings.TrimSuffix(lstFile[i], ext)
	}
	return lstFile
}

func buildFilename(path, disc string) (string, string, string) {
	dir, file := filepath.Split(path)
	ext := filepath.Ext(file)
	filename := strings.TrimSuffix(file, ext)
	glob := dir + "/" + filename + "__"+disc+"." + ext
	return filename, ext, glob
}

func kioReadFile(base string, file string) ([]*yaml.RNode, error) {
	filebytes, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("reading YAML file: %w", err)
	}

	path, err := filepath.Rel(base, file)
	if err != nil {
		return nil, fmt.Errorf("relativising path: %w", err)
	}
	annotations := map[string]string{
		kioutil.PathAnnotation: path,
	}

	rdr := &kio.ByteReader{
		Reader:         bytes.NewBuffer(filebytes),
		SetAnnotations: annotations,
	}

	nodes, err := rdr.Read()
	// Having screened the file and decided it's worth examining,
	// an error at this point is most unfortunate. However, it
	// doesn't need to be the end of the matter; we can record
	// this file as problematic, and continue.
	if err != nil {
		return nil, err
	}

	return nodes, nil
}