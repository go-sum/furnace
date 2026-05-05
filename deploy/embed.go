package deploy

import _ "embed"

//go:embed proxy/compose.yml
var ProxyComposeYML []byte

//go:embed furnace-worker.service
var WorkerServiceTemplate string
