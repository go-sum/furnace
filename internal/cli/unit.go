package cli

import (
	"bytes"
	"text/template"

	"github.com/go-sum/furnace/deploy"
)

type unitTemplateData struct {
	HasCredential bool
}

func renderWorkerUnit(hasCredential bool) ([]byte, error) {
	tmpl, err := template.New("worker-unit").Parse(deploy.WorkerServiceTemplate)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, unitTemplateData{HasCredential: hasCredential}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
