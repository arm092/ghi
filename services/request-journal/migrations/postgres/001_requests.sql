CREATE TABLE requests (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    title TEXT NOT NULL CHECK(length(trim(title)) BETWEEN 1 AND 200),
    status TEXT NOT NULL CHECK(status IN ('open', 'in_progress', 'resolved')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX requests_status_id ON requests(status, id DESC);
CREATE TABLE request_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    request_id BIGINT NOT NULL REFERENCES requests(id) ON DELETE CASCADE,
    status TEXT NOT NULL CHECK(status IN ('open', 'in_progress', 'resolved')),
    created_at TEXT NOT NULL
);
CREATE INDEX request_events_request_id ON request_events(request_id, id);
