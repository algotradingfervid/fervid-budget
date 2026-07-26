package store

const schemaSQL = `
PRAGMA foreign_keys=ON;

CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY,
  email TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  password_hash TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT 'data_entry',
  active INTEGER NOT NULL DEFAULT 1,
  attempt_count INTEGER NOT NULL DEFAULT 0,
  last_attempt DATETIME,
  locked DATETIME,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS projects (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  active INTEGER NOT NULL DEFAULT 1,
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS heads (
  id INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects(id),
  name TEXT NOT NULL,
  due_day TEXT,
  active INTEGER NOT NULL DEFAULT 1,
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(project_id, name)
);

CREATE TABLE IF NOT EXISTS budgets (
  id INTEGER PRIMARY KEY,
  head_id INTEGER NOT NULL REFERENCES heads(id),
  month TEXT NOT NULL,
  amount INTEGER NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(head_id, month)
);

CREATE TABLE IF NOT EXISTS budget_months (
  month TEXT PRIMARY KEY,
  status TEXT NOT NULL DEFAULT 'open',
  source_month TEXT,
  created_by INTEGER REFERENCES users(id),
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS payments (
  id INTEGER PRIMARY KEY,
  head_id INTEGER NOT NULL REFERENCES heads(id),
  paid_on TEXT NOT NULL,
  amount INTEGER NOT NULL,
  vendor_payee TEXT,
  payment_mode TEXT,
  invoice_no TEXT,
  reference_no TEXT,
  remarks TEXT,
  entered_by INTEGER NOT NULL REFERENCES users(id),
  updated_by INTEGER REFERENCES users(id),
  voided_by INTEGER REFERENCES users(id),
  void_reason TEXT,
  voided_at DATETIME,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS payment_attachments (
  id INTEGER PRIMARY KEY,
  payment_id INTEGER NOT NULL REFERENCES payments(id),
  original_name TEXT NOT NULL,
  stored_path TEXT NOT NULL,
  mime_type TEXT,
  size_bytes INTEGER NOT NULL,
  uploaded_by INTEGER NOT NULL REFERENCES users(id),
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS month_locks (
  month TEXT PRIMARY KEY,
  locked_by INTEGER NOT NULL REFERENCES users(id),
  locked_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  reason TEXT
);

CREATE TABLE IF NOT EXISTS audit_log (
  id INTEGER PRIMARY KEY,
  actor_id INTEGER REFERENCES users(id),
  actor_name TEXT,
  action TEXT NOT NULL,
  entity_type TEXT,
  entity_id INTEGER,
  summary TEXT,
  before_json TEXT,
  after_json TEXT,
  ip TEXT,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_payments_head ON payments(head_id);
CREATE INDEX IF NOT EXISTS idx_payments_paid_on ON payments(paid_on);
CREATE INDEX IF NOT EXISTS idx_payments_voided ON payments(voided_at);
CREATE INDEX IF NOT EXISTS idx_budgets_head_month ON budgets(head_id, month);
CREATE INDEX IF NOT EXISTS idx_budget_months_status ON budget_months(status);
CREATE INDEX IF NOT EXISTS idx_audit_entity ON audit_log(entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_log(created_at);
CREATE INDEX IF NOT EXISTS idx_attachments_payment ON payment_attachments(payment_id);
-- The original table constraints are retained for compatibility with existing
-- databases.  These expression indexes make identifiers case-insensitive too.
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email_nocase ON users(lower(email));
CREATE UNIQUE INDEX IF NOT EXISTS idx_projects_name_nocase ON projects(lower(name));
CREATE UNIQUE INDEX IF NOT EXISTS idx_heads_project_name_nocase ON heads(project_id, lower(name));
CREATE INDEX IF NOT EXISTS idx_users_login_lock ON users(email, locked);
`
