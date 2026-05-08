INSERT OR IGNORE INTO apps (name, image, tag_pattern, allowed_identity, artifact, domain, dir, port, tls, env_file, image_var, container, health_timeout, keep_releases)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
