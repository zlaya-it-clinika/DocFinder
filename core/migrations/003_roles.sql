CREATE TABLE IF NOT EXISTS roles (
	role_id text PRIMARY KEY,
	title text NOT NULL,
	description text NOT NULL DEFAULT '',
	system boolean NOT NULL DEFAULT false
);

CREATE TABLE IF NOT EXISTS role_permissions (
	role_id text NOT NULL REFERENCES roles(role_id) ON DELETE CASCADE,
	permission text NOT NULL,
	PRIMARY KEY (role_id, permission)
);

CREATE TABLE IF NOT EXISTS user_roles (
	user_id text NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
	role_id text NOT NULL REFERENCES roles(role_id) ON DELETE CASCADE,
	PRIMARY KEY (user_id, role_id)
);

CREATE TABLE IF NOT EXISTS document_roles (
	document_id text NOT NULL REFERENCES documents(document_id) ON DELETE CASCADE,
	role_id text NOT NULL REFERENCES roles(role_id) ON DELETE CASCADE,
	PRIMARY KEY (document_id, role_id)
);

ALTER TABLE documents ADD COLUMN IF NOT EXISTS visible_to_all boolean NOT NULL DEFAULT true;

CREATE TABLE IF NOT EXISTS permission_events (
	event_id text PRIMARY KEY,
	actor_user_id text NOT NULL,
	action text NOT NULL,
	role_id text NOT NULL DEFAULT '',
	target_user_id text NOT NULL DEFAULT '',
	created_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO id_counters (name, value) VALUES
	('role', 1),
	('permission_event', 1)
ON CONFLICT (name) DO NOTHING;
