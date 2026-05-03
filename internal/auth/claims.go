package auth

type GitHubClaims struct {
	Subject         string `json:"sub"`
	Repository      string `json:"repository"`
	RepositoryOwner string `json:"repository_owner"`
	Ref             string `json:"ref"`
	SHA             string `json:"sha"`
	Workflow        string `json:"workflow"`
	WorkflowRef     string `json:"workflow_ref"`
	JobWorkflowRef  string `json:"job_workflow_ref"`
	Actor           string `json:"actor"`
	RunID           string `json:"run_id"`
	EventName       string `json:"event_name"`
}
