package provisioner

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"path"
	"path/filepath"
	"strings"
	"text/template"
)

type templateContext struct {
	manifestData          map[string]string
	baseDir               string
	computingManifestHash bool
	readTemplate          func(string) ([]byte, error)
}

func newTemplateContext(baseDir string) *templateContext {
	return &templateContext{
		baseDir:      baseDir,
		manifestData: make(map[string]string),
	}
}

// renderTemplate takes a fileName of a template and the model to apply to it.
// returns the transformed template or an error if not successful
func renderTemplate(context *templateContext, filePath string, data interface{}) (string, error) {
	funcMap := template.FuncMap{
		"getAWSAccountID": getAWSAccountID,
		"base64":          base64Encode,
		"manifestHash":    func(template string) (string, error) { return manifestHash(context, filePath, template, data) },
	}

	content, err := ioutil.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	t, err := template.New(filePath).Option("missingkey=error").Funcs(funcMap).Parse(string(content))
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	err = t.Execute(&out, data)
	if err != nil {
		return "", err
	}

	templateData := out.String()
	context.manifestData[filePath] = templateData

	return templateData, nil
}

// manifestHash is a function for the templates that will return a hash of an interpolated sibling template
// file. returns an error if computing manifestHash calls manifestHash again, if interpolation of that template
// returns an error, or if the path is outside of the manifests folder.
func manifestHash(context *templateContext, file string, template string, data interface{}) (string, error) {
	if context.computingManifestHash {
		return "", fmt.Errorf("manifestHash is not reentrant")
	}
	context.computingManifestHash = true
	defer func() {
		context.computingManifestHash = false
	}()

	templateFile, err := filepath.Abs(path.Clean(path.Join(path.Dir(file), template)))
	if err != nil {
		return "", err
	}

	if !strings.HasPrefix(templateFile, context.baseDir) {
		return "", fmt.Errorf("invalid template path: %s", templateFile)
	}

	templateData, ok := context.manifestData[templateFile]
	if !ok {
		applied, err := renderTemplate(context, templateFile, data)
		if err != nil {
			return "", err
		}
		templateData = applied
	}

	return fmt.Sprintf("%x", sha256.Sum256([]byte(templateData))), nil
}

// getAWSAccountID is an utility function for the gotemplate that will remove
// the prefix "aws" from the infrastructure ID.
// TODO: get the real AWS account ID from the `external_id` field of the
// infrastructure account in the cluster registry.
func getAWSAccountID(ia string) string {
	return strings.Split(ia, ":")[1]
}

// base64Encode base64 encodes a string.
func base64Encode(value string) string {
	return base64.StdEncoding.EncodeToString([]byte(value))
}
