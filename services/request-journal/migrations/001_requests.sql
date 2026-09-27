CREATE TABLE requests (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT NOT NULL CHECK(length(trim(title)) BETWEEN 1 AND 200),
    status TEXT NOT NULL CHECK(status IN ('open', 'in_progress', 'resolved')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX requests_status_id ON requests(status, id DESC);
CREATE TABLE request_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    request_id INTEGER NOT NULL REFERENCES requests(id) ON DELETE CASCADE,
    status TEXT NOT NULL CHECK(status IN ('open', 'in_progress', 'resolved')),
    created_at TEXT NOT NULL
);
CREATE INDEX request_events_request_id ON request_events(request_id, id);
