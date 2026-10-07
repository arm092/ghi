DROP INDEX requests_owner_id;
ALTER TABLE requests DROP COLUMN user_id;
DROP TABLE sessions;
DROP TABLE users;
