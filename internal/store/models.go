package store

import "database/sql"

type ModelConfig struct {
	ContextTokens int     `json:"contextTokens"`
	TokenRatio    float64 `json:"-"`
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Provider      string  `json:"provider"`
	BaseURL       string  `json:"baseURL"`
	Model         string  `json:"model"`
	KeyRef        string  `json:"-"`
	HasKey        bool    `json:"hasKey"`
	Version       int     `json:"version"`
	AgentCount    int     `json:"agentCount"`
}

const modelSelect = `SELECT m.id,m.name,m.provider,m.base_url,m.model,m.key_ref,m.version,m.context_tokens,m.token_ratio,(SELECT count(*) FROM agents a WHERE a.model_id=m.id) FROM model_configs m`

func scanModel(row interface{ Scan(...any) error }) (ModelConfig, error) {
	var m ModelConfig
	err := row.Scan(&m.ID, &m.Name, &m.Provider, &m.BaseURL, &m.Model, &m.KeyRef, &m.Version, &m.ContextTokens, &m.TokenRatio, &m.AgentCount)
	if err == sql.ErrNoRows {
		return m, ValidationError("模型配置不存在，请重新选择")
	}
	m.HasKey = m.KeyRef != ""
	return m, err
}

func (s *Store) Models() ([]ModelConfig, error) {
	rows, err := s.db.Query(modelSelect + ` ORDER BY m.created_at,m.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []ModelConfig{}
	for rows.Next() {
		m, err := scanModel(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, m)
	}
	return list, rows.Err()
}

func (s *Store) Model(id string) (ModelConfig, error) {
	return scanModel(s.db.QueryRow(modelSelect+` WHERE m.id=?`, id))
}

func (s *Store) SaveModel(m ModelConfig) (ModelConfig, error) {
	if m.ContextTokens == 0 {
		m.ContextTokens = 32768
	}
	if m.ContextTokens < 8192 || m.ContextTokens > 2000000 {
		return m, ValidationError("上下文容量请输入 8192–2000000 Tokens")
	}
	now := timestamp()
	var err error
	if m.Version == 0 {
		_, err = s.db.Exec(`INSERT INTO model_configs(id,name,provider,base_url,model,key_ref,version,created_at,updated_at,context_tokens) VALUES(?,?,?,?,?,?,1,?,?,?)`, m.ID, m.Name, m.Provider, m.BaseURL, m.Model, m.KeyRef, now, now, m.ContextTokens)
	} else {
		var result sql.Result
		result, err = s.db.Exec(`UPDATE model_configs SET name=?,provider=?,base_url=?,model=?,key_ref=?,context_tokens=?,token_ratio=CASE WHEN model=? AND base_url=? THEN token_ratio ELSE 1 END,version=version+1,updated_at=? WHERE id=? AND version=?`, m.Name, m.Provider, m.BaseURL, m.Model, m.KeyRef, m.ContextTokens, m.Model, m.BaseURL, now, m.ID, m.Version)
		if err == nil {
			n, _ := result.RowsAffected()
			if n != 1 {
				return m, ValidationError("模型配置已被修改，请重新打开后编辑")
			}
		}
	}
	if err != nil {
		return m, err
	}
	return s.Model(m.ID)
}

func (s *Store) DeleteModel(id string, version int) error {
	result, err := s.db.Exec(`DELETE FROM model_configs WHERE id=? AND version=? AND NOT EXISTS(SELECT 1 FROM agents WHERE model_id=?)`, id, version, id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return ValidationError("模型配置已被修改或仍有角色使用，请刷新并先为角色切换模型")
	}
	return nil
}
