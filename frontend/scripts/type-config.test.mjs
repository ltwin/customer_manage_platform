import assert from 'node:assert/strict'
import test from 'node:test'
import ts from 'typescript'
import { fileURLToPath } from 'node:url'

test('application compiler keeps all effective strict checks enabled', () => {
  const file = fileURLToPath(new URL('../tsconfig.app.json', import.meta.url))
  const config = ts.getParsedCommandLineOfConfigFile(file, {}, {
    ...ts.sys,
    onUnRecoverableConfigFileDiagnostic(diagnostic) { throw new Error(ts.flattenDiagnosticMessageText(diagnostic.messageText, '\n')) },
  })
  assert.ok(config)
  assert.deepEqual(config.errors, [])
  assert.equal(config.options.strict, true)
  for (const flag of ['noImplicitAny', 'noImplicitThis', 'strictNullChecks', 'strictFunctionTypes', 'strictBindCallApply', 'strictPropertyInitialization', 'strictBuiltinIteratorReturn', 'alwaysStrict', 'useUnknownInCatchVariables']) {
    assert.equal(config.options[flag] ?? config.options.strict, true, flag)
  }
})
