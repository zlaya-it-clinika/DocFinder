-- Одна роль на пользователя. Из нескольких остаётся главный администратор, затем администратор, затем первая по коду
DELETE FROM user_roles ur
USING (
	SELECT user_id, role_id, row_number() OVER (
		PARTITION BY user_id
		ORDER BY CASE role_id WHEN 'role-chief' THEN 0 WHEN 'role-admin' THEN 1 ELSE 2 END, role_id
	) AS position
	FROM user_roles
) ranked
WHERE ur.user_id = ranked.user_id AND ur.role_id = ranked.role_id AND ranked.position > 1;

CREATE UNIQUE INDEX IF NOT EXISTS user_roles_single ON user_roles (user_id);
