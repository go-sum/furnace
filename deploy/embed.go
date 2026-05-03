package deploy

import _ "embed"

//go:embed furnace.service
var SystemdUnit []byte

//go:embed proxy/compose.yml
var ProxyComposeYML []byte
