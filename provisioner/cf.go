package provisioner

type stackPolicyEffect string
type stackPolicyAction string
type stackPolicyPrincipal string

const (
	stackPolicyEffectDeny  stackPolicyEffect = "Deny"
	stackPolicyEffectAllow stackPolicyEffect = "Allow"

	stackPolicyActionUpdateModify  stackPolicyAction = "Update:Modify"
	stackPolicyActionUpdateReplace stackPolicyAction = "Update:Replace"
	stackPolicyActionUpdateDelete  stackPolicyAction = "Update:Delete"
	stackPolicyActionUpdateAll     stackPolicyAction = "Update:*"

	stackPolicyPrincipalAll stackPolicyPrincipal = "*"
)

type stackPolicyConditionStringEquals struct {
	ResourceType []string `json:"ResourceType"`
}

type stackPolicyCondition struct {
	StringEquals stackPolicyConditionStringEquals `json:"StringEquals"`
}

type stackPolicyStatement struct {
	Effect      stackPolicyEffect     `json:"Effect"`
	Action      []stackPolicyAction   `json:"Action,omitempty"`
	Principal   stackPolicyPrincipal  `json:"Principal"`
	Resource    []string              `json:"Resource,omitempty"`
	NotResource []string              `json:"NotResource,omitempty"`
	Condition   *stackPolicyCondition `json:"Condition,omitempty"`
}

type stackPolicy struct {
	Statements []stackPolicyStatement `json:"Statement"`
}
