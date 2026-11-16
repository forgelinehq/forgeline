package blueprint

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// withCluster is a blueprint with an EKS cluster and two namespaces.
func withCluster() Blueprint {
	return Blueprint{ID: "bp-eks", Name: "Cluster", Target: Target{Cloud: "aws", Account: "primary", Region: "ap-south-1"},
		Blocks: []Block{{ID: "k8s", Kind: KindTemplate, Name: "Kube", Template: "aws-eks-cluster@1.0.0", Inputs: map[string]any{
			"namespaces":        map[string]any{"shop": "small", "data": "medium"},
			"namespace_editors": map[string]any{"shop": "arn:aws:iam::111122223333:role/shop-team"},
			"nodes":             1, "instance_type": "t4g.small",
		}}}}
}

// clusterState is an OpenTofu state holding the cluster of block k8s.
const clusterState = `{"version":4,"resources":[{"module":"module.k8s","mode":"managed","type":"aws_eks_cluster","name":"this","instances":[{"attributes":{}}]}]}`

type eksDoc struct {
	Terraform struct {
		RequiredProviders map[string]any `json:"required_providers"`
	} `json:"terraform"`
	Provider map[string]json.RawMessage           `json:"provider"`
	Module   map[string]map[string]any            `json:"module"`
	Data     map[string]map[string]map[string]any `json:"data"`
}

func readEKS(t *testing.T, code []byte) (eksDoc, []map[string]any) {
	t.Helper()
	var d eksDoc
	if err := json.Unmarshal(code, &d); err != nil {
		t.Fatal(err)
	}
	var kube []map[string]any
	json.Unmarshal(d.Provider["kubernetes"], &kube)
	return d, kube
}

func TestClusterIsBuiltInTwoStages(t *testing.T) {
	b := withCluster()
	if err := b.Validate(); err != nil {
		t.Fatal(err)
	}
	// Before the cluster exists: no namespaces, and an empty provider the module is handed.
	first, err := b.TofuFrom(nil)
	if err != nil {
		t.Fatal(err)
	}
	d, kube := readEKS(t, first)
	m := d.Module["k8s"]
	if m["cluster_ready"] != false || len(kube) != 1 || len(kube[0]) != 1 || kube[0]["alias"] != "k8s" || d.Data["aws_eks_cluster"] != nil {
		t.Errorf("first stage: module %v, provider %v, data %v", m["cluster_ready"], kube, d.Data["aws_eks_cluster"])
	}
	if p, _ := json.Marshal(m["providers"]); string(p) != `{"aws":"aws","kubernetes":"kubernetes.k8s"}` {
		t.Errorf("providers = %s", p)
	}
	if d.Terraform.RequiredProviders["kubernetes"] == nil {
		t.Error("the kubernetes provider is not required")
	}
	// Once it does: the namespaces, and a provider that reads the cluster by name and signs in when it runs.
	second, err := b.TofuFrom([]byte(clusterState))
	if err != nil {
		t.Fatal(err)
	}
	d, kube = readEKS(t, second)
	name := d.Module["k8s"]["name"].(string)
	if d.Module["k8s"]["cluster_ready"] != true || d.Data["aws_eks_cluster"]["k8s"]["name"] != name {
		t.Errorf("second stage: ready %v, data %v", d.Module["k8s"]["cluster_ready"], d.Data["aws_eks_cluster"])
	}
	p, _ := json.Marshal(kube)
	for _, want := range []string{`"host":"${data.aws_eks_cluster.k8s.endpoint}"`, `"command":"aws"`, `"get-token","--cluster-name","` + name + `","--region","ap-south-1"`} {
		if !strings.Contains(string(p), want) {
			t.Errorf("provider %s lacks %s", p, want)
		}
	}
	// Another block's cluster, or a cluster outside a module, is not this one.
	for _, other := range []string{
		`{"resources":[{"module":"module.other","mode":"managed","type":"aws_eks_cluster","name":"this","instances":[{}]}]}`,
		`{"resources":[{"mode":"managed","type":"aws_eks_cluster","name":"this","instances":[{}]}]}`,
		`{"resources":[{"module":"module.k8s","mode":"data","type":"aws_eks_cluster","name":"this","instances":[{}]}]}`,
		`{"resources":[{"module":"module.k8s","mode":"managed","type":"aws_eks_cluster","name":"this","instances":[]}]}`,
	} {
		code, _ := b.TofuFrom([]byte(other))
		if d, _ := readEKS(t, code); d.Module["k8s"]["cluster_ready"] != false {
			t.Errorf("ready from %s", other)
		}
	}
	if _, err := b.TofuFrom([]byte("not json")); err == nil {
		t.Error("an unreadable state was accepted")
	}
}

func TestClusterRemovalKeepsItsProvider(t *testing.T) {
	b := withCluster()
	for _, stage := range []string{RemoveAll, RemoveKeepData} {
		code, err := b.RemovalTofuFrom(stage, false, []byte(clusterState))
		if err != nil {
			t.Fatal(err)
		}
		d, kube := readEKS(t, code)
		if d.Module["k8s"] != nil || len(kube) != 1 || kube[0]["host"] == nil || d.Data["aws_eks_cluster"]["k8s"] == nil || d.Terraform.RequiredProviders["kubernetes"] == nil {
			t.Errorf("%s: module %v, provider %v", stage, d.Module, kube)
		}
	}
	// Nothing of the cluster built: nothing to reach.
	code, _ := b.RemovalTofuFrom(RemoveAll, false, nil)
	if d, kube := readEKS(t, code); kube != nil || d.Terraform.RequiredProviders["kubernetes"] != nil {
		t.Errorf("removal before a cluster: %s", code)
	}
}

// TestClusterCodeValidates runs tofu validate on both stages and the removal, with the module beside them.
func TestClusterCodeValidates(t *testing.T) {
	bin := os.Getenv("FORGELINE_TOFU")
	if bin == "" {
		t.Skip("set FORGELINE_TOFU to a tofu binary to check the code with tofu validate")
	}
	b := withCluster()
	first, _ := b.TofuFrom(nil)
	second, _ := b.TofuFrom([]byte(clusterState))
	removal, _ := b.RemovalTofuFrom(RemoveAll, false, []byte(clusterState))
	for name, code := range map[string][]byte{"first": first, "second": second, "removal": removal} {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "main.tf.json"), code, 0o600)
		mods, err := ModulesFor(code)
		if err != nil {
			t.Fatal(err)
		}
		for p, body := range mods {
			os.MkdirAll(filepath.Join(dir, filepath.Dir(p)), 0o700)
			os.WriteFile(filepath.Join(dir, p), []byte(body), 0o600)
		}
		for _, args := range [][]string{{"init", "-backend=false", "-input=false", "-no-color"}, {"validate", "-no-color"}} {
			cmd := exec.Command(bin, args...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s: tofu %s: %v\n%s", name, args[0], err, out)
			}
		}
	}
}
