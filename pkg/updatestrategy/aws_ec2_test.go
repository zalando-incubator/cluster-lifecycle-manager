package updatestrategy

import (
	"testing"

	karpenterawsv1 "github.com/aws/karpenter-provider-aws/pkg/apis/v1"
	"github.com/awslabs/operatorpkg/status"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

func TestNodeClaimNameFromOwnerReferences(t *testing.T) {
	node := corev1.Node{ObjectMeta: metav1.ObjectMeta{OwnerReferences: []metav1.OwnerReference{
		{Kind: "ReplicaSet", Name: "rs-a"},
		{Kind: "NodeClaim", Name: "claim-a"},
	}}}

	nodeClaimName, ok := nodeClaimNameFromOwnerReferences(node)
	if !ok {
		t.Fatalf("expected NodeClaim owner reference to be found")
	}
	if nodeClaimName != "claim-a" {
		t.Fatalf("expected node claim name claim-a, got %q", nodeClaimName)
	}
}

func TestNodeClaimNameFromOwnerReferencesMissing(t *testing.T) {
	node := corev1.Node{ObjectMeta: metav1.ObjectMeta{OwnerReferences: []metav1.OwnerReference{
		{Kind: "ReplicaSet", Name: "rs-a"},
	}}}

	nodeClaimName, ok := nodeClaimNameFromOwnerReferences(node)
	if ok {
		t.Fatalf("expected NodeClaim owner reference to be absent, got %q", nodeClaimName)
	}
}

func TestNodeClaimDriftMaps(t *testing.T) {
	nodeClaims := []karpv1.NodeClaim{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "claim-a"},
			Status: karpv1.NodeClaimStatus{
				NodeName: "node-a",
				Conditions: []status.Condition{
					{Type: string(karpv1.ConditionTypeDrifted), Status: metav1.ConditionTrue},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "claim-b"},
			Status: karpv1.NodeClaimStatus{
				NodeName: "node-b",
				Conditions: []status.Condition{
					{Type: string(karpv1.ConditionTypeDrifted), Status: metav1.ConditionFalse},
				},
			},
		},
	}

	nodePool := &karpv1.NodePool{}
	ec2NodeClassAnnotations := map[string]string{}
	driftByNodeClaimName, driftByNodeName := nodeClaimDriftMaps(nodeClaims, nodePool, ec2NodeClassAnnotations)

	if !driftByNodeClaimName["claim-a"] {
		t.Fatalf("expected claim-a to be drifted")
	}
	if driftByNodeClaimName["claim-b"] {
		t.Fatalf("expected claim-b to not be drifted")
	}

	if !driftByNodeName["node-a"] {
		t.Fatalf("expected node-a to be drifted")
	}
	if driftByNodeName["node-b"] {
		t.Fatalf("expected node-b to not be drifted")
	}
}

func TestNodeClaimDriftMapsByHashMismatch(t *testing.T) {
	nodePool := &karpv1.NodePool{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
			karpv1.NodePoolHashAnnotationKey:        "hash-current",
			karpv1.NodePoolHashVersionAnnotationKey: "v3",
		}},
	}

	ec2NodeClassAnnotations := map[string]string{
		karpenterawsv1.AnnotationEC2NodeClassHash:        "class-hash-current",
		karpenterawsv1.AnnotationEC2NodeClassHashVersion: "v3",
	}

	nodeClaims := []karpv1.NodeClaim{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "claim-a",
				Annotations: map[string]string{
					karpv1.NodePoolHashAnnotationKey:                 "hash-old",
					karpv1.NodePoolHashVersionAnnotationKey:          "v3",
					karpenterawsv1.AnnotationEC2NodeClassHash:        "class-hash-current",
					karpenterawsv1.AnnotationEC2NodeClassHashVersion: "v3",
				},
			},
			Status: karpv1.NodeClaimStatus{
				NodeName: "node-a",
				Conditions: []status.Condition{
					{Type: string(karpv1.ConditionTypeDrifted), Status: metav1.ConditionFalse},
				},
			},
		},
	}

	driftByNodeClaimName, driftByNodeName := nodeClaimDriftMaps(nodeClaims, nodePool, ec2NodeClassAnnotations)

	if !driftByNodeClaimName["claim-a"] {
		t.Fatalf("expected claim-a to be drifted by hash mismatch")
	}

	if !driftByNodeName["node-a"] {
		t.Fatalf("expected node-a to be drifted by hash mismatch")
	}
}

func TestIsNodeClaimBehindObservedTemplates(t *testing.T) {
	tests := []struct {
		name                 string
		nodeClaim            *karpv1.NodeClaim
		nodePool             *karpv1.NodePool
		nodeClassAnnotations map[string]string
		expected             bool
	}{
		{
			name: "nodepool hash mismatch",
			nodeClaim: &karpv1.NodeClaim{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
				karpv1.NodePoolHashAnnotationKey:                 "old",
				karpv1.NodePoolHashVersionAnnotationKey:          "v3",
				karpenterawsv1.AnnotationEC2NodeClassHash:        "same",
				karpenterawsv1.AnnotationEC2NodeClassHashVersion: "v3",
			}}},
			nodePool: &karpv1.NodePool{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
				karpv1.NodePoolHashAnnotationKey:        "new",
				karpv1.NodePoolHashVersionAnnotationKey: "v3",
			}}},
			nodeClassAnnotations: map[string]string{
				karpenterawsv1.AnnotationEC2NodeClassHash:        "same",
				karpenterawsv1.AnnotationEC2NodeClassHashVersion: "v3",
			},
			expected: true,
		},
		{
			name: "no mismatch",
			nodeClaim: &karpv1.NodeClaim{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
				karpv1.NodePoolHashAnnotationKey:                 "same",
				karpv1.NodePoolHashVersionAnnotationKey:          "v3",
				karpenterawsv1.AnnotationEC2NodeClassHash:        "same",
				karpenterawsv1.AnnotationEC2NodeClassHashVersion: "v3",
			}}},
			nodePool: &karpv1.NodePool{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
				karpv1.NodePoolHashAnnotationKey:        "same",
				karpv1.NodePoolHashVersionAnnotationKey: "v3",
			}}},
			nodeClassAnnotations: map[string]string{
				karpenterawsv1.AnnotationEC2NodeClassHash:        "same",
				karpenterawsv1.AnnotationEC2NodeClassHashVersion: "v3",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := isNodeClaimBehindObservedTemplates(tt.nodeClaim, tt.nodePool, tt.nodeClassAnnotations)
			if actual != tt.expected {
				t.Fatalf("expected isNodeClaimBehindObservedTemplates=%t, got %t", tt.expected, actual)
			}
		})
	}
}

func TestIsNodeClaimDrifted(t *testing.T) {
	tests := []struct {
		name      string
		nodeClaim *karpv1.NodeClaim
		expected  bool
	}{
		{
			name: "drifted condition true",
			nodeClaim: &karpv1.NodeClaim{Status: karpv1.NodeClaimStatus{Conditions: []status.Condition{
				{Type: string(karpv1.ConditionTypeDrifted), Status: metav1.ConditionTrue},
			}}},
			expected: true,
		},
		{
			name: "drifted condition false",
			nodeClaim: &karpv1.NodeClaim{Status: karpv1.NodeClaimStatus{Conditions: []status.Condition{
				{Type: string(karpv1.ConditionTypeDrifted), Status: metav1.ConditionFalse},
			}}},
			expected: false,
		},
		{
			name:      "missing conditions",
			nodeClaim: &karpv1.NodeClaim{},
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := isNodeClaimDrifted(tt.nodeClaim)
			if actual != tt.expected {
				t.Fatalf("expected isNodeClaimDrifted=%t, got %t", tt.expected, actual)
			}
		})
	}
}
