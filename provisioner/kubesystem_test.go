package provisioner

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"testing"

	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/stretchr/testify/require"
)

const hashedTmpl = `foo: {{ .LocalID }}`

const tmpl = `apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: mate
  namespace: kube-system
  labels:
    application: mate
    version: v0.5.1
spec:
  replicas: 1
  selector:
    matchLabels:
      application: mate
  template:
    metadata:
      labels:
        application: mate
        version: v0.5.1
      annotations:
        scheduler.alpha.kubernetes.io/critical-pod: ''
        scheduler.alpha.kubernetes.io/tolerations: '[{"key":"CriticalAddonsOnly", "operator":"Exists"}]'
        iam.amazonaws.com/role: "{{ .LocalID }}-app-mate"
        config/hash: {{ "test_hashed" | manifestHash }}
    spec:
      containers:
      - name: mate
        image: registry.opensource.zalan.do/teapot/mate:v0.5.1
        env:
        - name: AWS_REGION
          value: {{ .Region }}
        args:
        - --producer=kubernetes
        - --kubernetes-format={{` + "`{{ .Name }}-{{ .Namespace }}`" + `}}.{{ .ConfigItems.mate_hosted_zone }}.
        - --consumer=aws
        - --aws-record-group-id={{ .LocalID }}
        resources:
          limits:
            cpu: 200m
            memory: 200Mi
          requests:
            cpu: 50m
            memory: 25Mi`

func TestApplyTemplate(t *testing.T) {
	ioutil.WriteFile("test_hashed", []byte(hashedTmpl), 0666)
	defer os.Remove("test_hashed")

	ioutil.WriteFile("test_template", []byte(tmpl), 0666)
	defer os.Remove("test_template")

	cdir, err := os.Getwd()
	require.NoError(t, err)
	context := newApplyContext(cdir)

	region := "eu-central"
	localID := "kube-aws-test-rdifazio55"
	cluster := &api.Cluster{Region: region, LocalID: localID}

	s, err := applyTemplate(context, "test_template", cluster)
	if err == nil {
		t.Errorf("should fail, mate hosted zone configitems are not passed!")
	}

	cluster.ConfigItems = map[string]string{
		"mate_hosted_zone": "hosted-zone",
	}
	s, err = applyTemplate(context, "test_template", cluster)
	if err != nil {
		t.Errorf("should not fail %v", err)
	}

	fail := strings.Contains(s, "{{ .Region }}")
	if fail {
		t.Errorf("contains string {{ .Region }}: %s", s)
	}
	fail = strings.Contains(s, "{{ .LocalID }}")
	if fail {
		t.Errorf("contains string {{ .LocalID }}: %s", s)
	}
	fail = strings.Contains(s, "{{ .ConfigItems.mate_hosted_zone }}")
	if fail {
		t.Errorf("contains string {{ .ConfigItems.mate_hosted_zone }}: %s", s)
	}
	replaced := strings.Contains(s, "{{ .Name }}-{{ .Namespace }}")
	if !replaced {
		t.Errorf("does not contain string {{ .Name }}-{{ .Namespace }} for mate config: %s", s)
	}
	replaced = strings.Contains(s, "kube-aws-test-rdifazio55")
	if !replaced {
		t.Errorf("does not contain kube-aws-test-rdifazio55: %s", s)
	}
	replaced = strings.Contains(s, "eu-central")
	if !replaced {
		t.Errorf("does not contain eu-central: %s", s)
	}
	expectedHash := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("foo: %s", cluster.LocalID))))
	replaced = strings.Contains(s, expectedHash)
	if !replaced {
		t.Errorf("does not contain replaced %s: %s", expectedHash, s)
	}
}

const tmplFunc = `{{ .ConfigItems.my_value | base64 }}`

func TestApplyTemplateBase64Fun(t *testing.T) {
	ioutil.WriteFile("test_hashed", []byte(hashedTmpl), 0666)
	defer os.Remove("test_hashed")

	ioutil.WriteFile("test_template", []byte(tmplFunc), 0666)
	defer os.Remove("test_template")

	cdir, err := os.Getwd()
	require.NoError(t, err)
	context := newApplyContext(cdir)

	value := "value"

	cluster := &api.Cluster{}
	cluster.ConfigItems = map[string]string{
		"my_value": value,
	}
	s, err := applyTemplate(context, "test_template", cluster)
	if err != nil {
		t.Errorf("should not fail %v", err)
	}

	encoded := base64.StdEncoding.EncodeToString([]byte(value))
	if encoded != s {
		t.Errorf("expected value %s, got %s", encoded, s)
	}
}

func TestLabelsString(t *testing.T) {
	labels := labels(map[string]string{"key": "value", "foo": "bar"})
	expected := []string{"key=value,foo=bar", "foo=bar,key=value"}
	labelStr := labels.String()
	if labelStr != expected[0] && labelStr != expected[1] {
		t.Errorf("expected labels format: %+v, got %+v", expected, labels)
	}
}

var deletionsContent = []byte(`
pre_apply:
- name: secretary
  namespace: kube-system
  kind: deployment
- name: mate
  kind: deployment
post_apply:
- name: secretary
  namespace: kube-system
  kind: deployment
- name: mate
  kind: deployment`)

func TestParseDeletions(t *testing.T) {
	err := ioutil.WriteFile(deletionsFile, deletionsContent, 0644)
	if err != nil {
		t.Errorf("should not fail: %s", err)
	}
	defer os.RemoveAll(deletionsFile)

	deletions, err := parseDeletions(".")
	if err != nil {
		t.Errorf("should not fail: %s", err)
	}

	if len(deletions.PreApply) != 2 {
		t.Errorf("expected %d PreApply deletions, got %d", 2, len(deletions.PreApply))
	}

	if len(deletions.PostApply) != 2 {
		t.Errorf("expected %d PostApply deletions, got %d", 2, len(deletions.PostApply))
	}

	// test not getting an error if file doesn't exists
	_, err = parseDeletions("invalid_folder")
	if err != nil {
		t.Errorf("should not fail: %s", err)
	}
}
