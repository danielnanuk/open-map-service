CREATE TABLE IF NOT EXISTS api_keys (
  key        text PRIMARY KEY,              -- 明文 key(单机自托管;轮换=插新删旧)
  name       text NOT NULL,
  rpm_limit  int  NOT NULL DEFAULT 300,     -- 每分钟请求数
  active     bool NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now()
);
