const {
  buildCodexUpstreamHeaders,
  compareVersions,
  isLegacyCodexCliUserAgent,
  parseCodexCliUserAgent
} = require('../src/utils/codexCliCompat')

describe('codexCliCompat', () => {
  describe('parseCodexCliUserAgent', () => {
    it('should return null for non-codex user-agent', () => {
      expect(parseCodexCliUserAgent('curl/8.0.0')).toBeNull()
    })

    it('should parse codex_cli_rs user-agent with version', () => {
      const info = parseCodexCliUserAgent('codex_cli_rs/0.63.0 (Ubuntu 22.4.0; x86_64)')
      expect(info).toEqual({ client: 'codex_cli_rs', version: '0.63.0' })
    })

    it('should parse codex_vscode user-agent with version', () => {
      const info = parseCodexCliUserAgent('codex_vscode/0.35.0 (Windows 10.0.26100; x86_64)')
      expect(info).toEqual({ client: 'codex_vscode', version: '0.35.0' })
    })
  })

  describe('compareVersions', () => {
    it('should compare dot versions correctly', () => {
      expect(compareVersions('0.63.0', '0.70.0')).toBe(-1)
      expect(compareVersions('0.70.0', '0.70.0')).toBe(0)
      expect(compareVersions('0.72.0', '0.70.0')).toBe(1)
      expect(compareVersions('0.70', '0.70.0')).toBe(0)
    })
  })

  describe('isLegacyCodexCliUserAgent', () => {
    it('should treat older versions as legacy', () => {
      expect(isLegacyCodexCliUserAgent('codex_cli_rs/0.63.0', '0.70.0')).toBe(true)
    })

    it('should not treat newer versions as legacy', () => {
      expect(isLegacyCodexCliUserAgent('codex_cli_rs/0.72.0', '0.70.0')).toBe(false)
    })

    it('should return false for non-codex user-agent', () => {
      expect(isLegacyCodexCliUserAgent('curl/8.0.0', '0.70.0')).toBe(false)
    })
  })

  describe('buildCodexUpstreamHeaders', () => {
    it('should map legacy header names to upstream headers', () => {
      const headers = buildCodexUpstreamHeaders({
        'X-Session-Id': 'sess_legacy_123',
        'OpenAI-Version': '2020-10-01',
        'OpenAI-Beta': 'responses=v1',
        Originator: 'codex_cli_rs'
      })

      expect(headers).toEqual({
        'openai-beta': 'responses=v1',
        originator: 'codex_cli_rs',
        'openai-version': '2020-10-01',
        version: '2020-10-01',
        session_id: 'sess_legacy_123'
      })
    })

    it('should prefer session_id when both are present', () => {
      const headers = buildCodexUpstreamHeaders({
        'x-session-id': 'sess_legacy_123',
        session_id: 'sess_new_456'
      })

      expect(headers).toEqual({ session_id: 'sess_new_456' })
    })

    it('should prefer version when both version and openai-version are present', () => {
      const headers = buildCodexUpstreamHeaders({
        version: 'v_custom',
        'openai-version': '2020-10-01'
      })

      expect(headers).toEqual({
        'openai-version': '2020-10-01',
        version: 'v_custom'
      })
    })
  })
})

