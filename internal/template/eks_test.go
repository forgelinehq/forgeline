package template

import (
	"encoding/json"
	"testing"
	"testing/fstest"
)

const eksDir = "library/aws-eks-cluster/1.0.0"

// eksWith is a copy of the EKS template with one file changed.
func eksWith(t *testing.T, file string, change func(map[string]any)) fstest.MapFS {
	t.Helper()
	m := fstest.MapFS{}
	for _, f := range []string{"template.json", "diagram.json", "main.tf.json"} {
		b, err := library.ReadFile(eksDir + "/" + f)
		if err != nil {
			t.Fatal(err)
		}
		if f == file {
			var doc map[string]any
			json.Unmarshal(b, &doc)
			change(doc)
			b, _ = json.Marshal(doc)
		}
		m["t/aws-eks-cluster/1.0.0/"+f] = &fstest.MapFile{Data: b}
	}
	return m
}

func TestKubernetesStageRules(t *testing.T) {
	e, err := Get("aws-eks-cluster@1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if e.Kubernetes == nil || e.Kubernetes.Cluster != "aws_eks_cluster.this" || len(e.AttachesPolicies) != 4 || e.PassesRoleTo[0] != "eks.amazonaws.com" {
		t.Fatalf("eks = %+v", e)
	}
	if _, err := Load(eksWith(t, "", nil), "t/aws-eks-cluster/1.0.0"); err != nil {
		t.Fatalf("the unchanged copy: %v", err)
	}
	res := func(d map[string]any, typ string) map[string]any { return d["resource"].(map[string]any)[typ].(map[string]any) }
	ns := func(d map[string]any) map[string]any { return res(d, "kubernetes_namespace_v1")["this"].(map[string]any) }
	attach := func(d map[string]any) map[string]any { return res(d, "aws_iam_role_policy_attachment")["cluster"].(map[string]any) }
	for name, c := range map[string]struct {
		file   string
		change func(map[string]any)
	}{
		"the provider without a stage": {"template.json", func(d map[string]any) { delete(d, "kubernetes") }},
		"a stage without the provider": {"template.json", func(d map[string]any) { delete(d["providers"].(map[string]any), "kubernetes") }},
		"a cluster of another kind":    {"template.json", func(d map[string]any) { d["kubernetes"] = map[string]any{"cluster": "aws_instance.this"} }},
		"a cluster not declared":       {"template.json", func(d map[string]any) { d["kubernetes"] = map[string]any{"cluster": "aws_eks_cluster.other"} }},
		"a cluster named otherwise":    {"main.tf.json", func(d map[string]any) { res(d, "aws_eks_cluster")["this"].(map[string]any)["name"] = "${var.name}-x" }},
		"a namespace made at once":     {"main.tf.json", func(d map[string]any) { ns(d)["for_each"] = "${var.namespaces}" }},
		"a namespace with no gate":     {"main.tf.json", func(d map[string]any) { delete(ns(d), "for_each") }},
		"a count gate that is not it":  {"main.tf.json", func(d map[string]any) { ns(d)["count"] = "${var.nodes > 0 ? 1 : 0}" }},
		"a namespace not after the cluster": {"main.tf.json", func(d map[string]any) {
			ns(d)["depends_on"] = []any{"aws_eks_addon.vpc_cni"}
		}},
		"reading from the cluster": {"main.tf.json", func(d map[string]any) {
			d["data"].(map[string]any)["kubernetes_namespace_v1"] = map[string]any{"x": map[string]any{"metadata": map[string]any{"name": "default"}}}
		}},
		"reading a Kubernetes secret": {"main.tf.json", func(d map[string]any) {
			d["data"].(map[string]any)["kubernetes_secret_v1"] = map[string]any{"x": map[string]any{"metadata": map[string]any{"name": "s"}}}
		}},
		"no cluster_ready variable": {"main.tf.json", func(d map[string]any) { delete(d["variable"].(map[string]any), "cluster_ready") }},
		"cluster_ready as an input": {"template.json", func(d map[string]any) {
			d["inputs"].(map[string]any)["properties"].(map[string]any)["cluster_ready"] = map[string]any{"type": "boolean", "default": true}
		}},
		"an unlisted policy":       {"main.tf.json", func(d map[string]any) { attach(d)["policy_arn"] = "arn:aws:iam::aws:policy/AmazonS3FullAccess" }},
		"a policy from a variable": {"main.tf.json", func(d map[string]any) { attach(d)["policy_arn"] = "${var.admin_role_arn}" }},
		"an attachment to anyone": {"main.tf.json", func(d map[string]any) {
			d["resource"].(map[string]any)["aws_iam_policy_attachment"] = map[string]any{"x": map[string]any{"name": "x", "policy_arn": "arn:aws:iam::aws:policy/AmazonEKSClusterPolicy"}}
		}},
		"listing AdministratorAccess": {"template.json", func(d map[string]any) {
			d["attaches_policies"] = append(d["attaches_policies"].([]any), "arn:aws:iam::aws:policy/AdministratorAccess")
		}},
		"listing an IAM policy": {"template.json", func(d map[string]any) {
			d["attaches_policies"] = append(d["attaches_policies"].([]any), "arn:aws:iam::aws:policy/IAMReadOnlyAccess")
		}},
		"listing an account's own policy": {"template.json", func(d map[string]any) {
			d["attaches_policies"] = append(d["attaches_policies"].([]any), "arn:aws:iam::111122223333:policy/mine")
		}},
	} {
		if _, err := Load(eksWith(t, c.file, c.change), "t/aws-eks-cluster/1.0.0"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
