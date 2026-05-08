package cli

const (
	// Root directories — the only independently-defined paths.
	DataDir  = "/var/lib/furnace"
	AppsDir  = "/srv/apps"
	InfraDir = "/srv/furnace"
	CredDir  = "/etc/furnace"

	// Derived from DataDir
	DBPath     = DataDir + "/furnace.db"
	CACertPath = DataDir + "/ca/ca.pem"
	CAKeyPath  = DataDir + "/ca/ca-key.pem"

	// Derived from InfraDir
	ProxyDir       = InfraDir + "/proxy"
	CertsDir       = InfraDir + "/certs"
	ServerCertPath = CertsDir + "/local.pem"
	ServerKeyPath  = CertsDir + "/local-key.pem"

	// Derived from CredDir
	CredPath = CredDir + "/registry-token.cred"

	// OS-mandated destinations (not furnace-controlled)
	SystemCADest   = "/usr/local/share/ca-certificates/furnace-ca.crt"
	WorkerUnitDest = "/etc/systemd/system/furnace-worker.service"
)
