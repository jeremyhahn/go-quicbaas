package iam

import "strconv"

// PolicyNameMismatchError reports a policy document whose metadata.name is
// not the name it is validated or stored under.
type PolicyNameMismatchError struct {
	// Requested is the name the caller will store the policy under.
	Requested string
	// Document is the metadata.name the document declares.
	Document string
}

// Error implements the error interface.
func (e *PolicyNameMismatchError) Error() string {
	return "policy metadata.name " + strconv.Quote(e.Document) +
		" does not match the request name " + strconv.Quote(e.Requested)
}

// CheckPolicyName checks policy against the name it will be stored under.
//
// An empty name checks nothing. A document that declares a different
// metadata.name returns an ErrPolicyInvalid-coded error wrapping
// *PolicyNameMismatchError. A document that declares no name takes name, as
// it would when stored.
func CheckPolicyName(policy *Policy, name string) error {
	if name == "" || policy == nil {
		// A nil policy has no name to check; PolicyEngine.ValidatePolicy
		// reports it.
		return nil
	}
	if policy.Metadata.Name != "" && policy.Metadata.Name != name {
		return NewIAMError(ErrPolicyInvalid, "iam.policy.name", "policy name mismatch",
			&PolicyNameMismatchError{Requested: name, Document: policy.Metadata.Name})
	}
	policy.Metadata.Name = name
	return nil
}
