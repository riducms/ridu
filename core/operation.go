package core

// Operation identifies a document operation at access and hook boundaries.
type Operation string

const (
	OperationCreate          Operation = "create"
	OperationDuplicate       Operation = "duplicate"
	OperationAdmin           Operation = "admin"
	OperationRead            Operation = "read"
	OperationReadVersions    Operation = "read-versions"
	OperationUpdate          Operation = "update"
	OperationDelete          Operation = "delete"
	OperationRestoreDeleted  Operation = "restore-deleted"
	OperationDeletePermanent Operation = "delete-permanent"
	OperationPublish         Operation = "publish"
	OperationUnpublish       Operation = "unpublish"
	OperationUnlock          Operation = "unlock"
)
