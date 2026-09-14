#!/usr/bin/env node

import {
  chmodSync, closeSync, constants, fstatSync, lstatSync, mkdtempSync, openSync,
  readFileSync, realpathSync, rmSync, writeFileSync,
} from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { spawnSync } from 'node:child_process'

const OUTPUT_LIMIT = 64 * 1024
const TIMEOUT_MS = 120_000
const IDENTIFIER = /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/

function report(classification, exitCode, echoDetected) {
  process.stdout.write(`${JSON.stringify({ classification, exit_code: exitCode, echo_detected: echoDetected })}\n`)
}

function options(values, allowed) {
  if (values.length % 2 !== 0) throw new Error('invalid')
  const parsed = new Map()
  for (let index = 0; index < values.length; index += 2) {
    const [name, value] = values.slice(index, index + 2)
    if (!allowed.has(name) || parsed.has(name) || !value || /[\0\r\n]/.test(value)) throw new Error('invalid')
    parsed.set(name, value)
  }
  return parsed
}

function absolute(parsed, name) {
  const value = parsed.get(name)
  if (!value?.startsWith('/')) throw new Error('invalid')
  return resolve(value)
}

function identifier(parsed, name, fallback) {
  const value = parsed.get(name) || fallback
  if (!IDENTIFIER.test(value)) throw new Error('invalid')
  return value
}

function commandSpec(argv) {
  if (argv[0] === 'init') {
    const parsed = options(argv.slice(1), new Set([
      '--binary', '--db', '--secret-file', '--owner-username', '--organization-id',
    ]))
    return {
      mode: 'init',
      secretFile: absolute(parsed, '--secret-file'),
      publicArgv: [absolute(parsed, '--binary'), 'init', '--db', absolute(parsed, '--db'),
        '--owner-username', identifier(parsed, '--owner-username', 'owner'),
        '--organization-id', identifier(parsed, '--organization-id', 'default')],
    }
  }
  if (argv[0] === 'agent-apply') {
    const parsed = options(argv.slice(1), new Set([
      '--binary', '--db', '--file', '--secret-file', '--owner-username',
    ]))
    return {
      mode: 'agent-apply',
      secretFile: absolute(parsed, '--secret-file'),
      publicArgv: [absolute(parsed, '--binary'), 'agent', 'apply', '--db', absolute(parsed, '--db'),
        '--file', absolute(parsed, '--file'), '--owner-username', identifier(parsed, '--owner-username', 'owner')],
    }
  }
  throw new Error('invalid')
}

function readPrivateSecret(path) {
  const parentPath = dirname(path)
  const parent = lstatSync(parentPath)
  const file = lstatSync(path)
  if (!parent.isDirectory() || parent.isSymbolicLink() || (parent.mode & 0o777) !== 0o700 ||
      !file.isFile() || file.isSymbolicLink() || (file.mode & 0o777) !== 0o600 ||
      dirname(realpathSync(path)) !== realpathSync(parentPath)) throw new Error('private input rejected')
  const descriptor = openSync(path, constants.O_RDONLY | constants.O_NOFOLLOW)
  try {
    const opened = fstatSync(descriptor)
    if (!opened.isFile() || (opened.mode & 0o777) !== 0o600 || opened.dev !== file.dev ||
        opened.ino !== file.ino || opened.size < 1 || opened.size > 4096) throw new Error('private input rejected')
    const secret = readFileSync(descriptor)
    if (secret.length < 1 || secret.length > 4096 || secret.includes(0) || secret.includes(10) || secret.includes(13)) {
      throw new Error('private input rejected')
    }
    return secret
  } finally {
    closeSync(descriptor)
  }
}

function shellQuote(value) {
  return `'${value.replaceAll("'", `'"'"'`)}'`
}

function runAdmin(spec, secret) {
  const newline = Buffer.from('\n')
  const input = spec.mode === 'init'
    ? Buffer.concat([secret, newline, secret, newline])
    : Buffer.concat([secret, newline])
  const child = spawnSync('script', [
    '--echo', 'never', '--quiet', '--return', '--command', spec.publicArgv.map(shellQuote).join(' '), '/dev/null',
  ], { input, maxBuffer: OUTPUT_LIMIT, timeout: TIMEOUT_MS, killSignal: 'SIGKILL' })
  input.fill(0)
  const stdout = Buffer.isBuffer(child.stdout) ? child.stdout : Buffer.alloc(0)
  const stderr = Buffer.isBuffer(child.stderr) ? child.stderr : Buffer.alloc(0)
  const echoDetected = stdout.includes(secret) || stderr.includes(secret)
  const exitCode = Number.isInteger(child.status) ? child.status : 1
  if (echoDetected) return { classification: 'secret_echo_detected', exitCode, echoDetected }
  if (child.error?.code === 'ENOBUFS') return { classification: 'output_limit', exitCode: 137, echoDetected }
  if (child.error?.code === 'ETIMEDOUT') return { classification: 'timeout', exitCode: 124, echoDetected }
  if (child.error) return { classification: 'launch_failed', exitCode: 126, echoDetected }
  if (child.signal) return { classification: 'signal_termination', exitCode: 128, echoDetected }
  if (child.status !== 0) return { classification: 'child_failed', exitCode, echoDetected }
  return { classification: 'success', exitCode: 0, echoDetected }
}

function selfTest() {
  const root = mkdtempSync(join(tmpdir(), "openagentx-private-admin-'selftest-"))
  try {
    chmodSync(root, 0o700)
    const binary = join(root, 'synthetic-admin')
    const secretFile = join(root, 'owner-password')
    const identity = join(root, 'identity.yaml')
    const database = join(root, 'fixture.db')
    const expected = Buffer.from('public-synthetic-password-123')
    writeFileSync(binary, `#!/bin/sh
IFS= read -r first || exit 21
if [ "$1" = "init" ]; then
  IFS= read -r second || exit 22
  [ "$first" = "$second" ] || exit 23
elif ! { [ "$1" = "agent" ] && [ "$2" = "apply" ]; }; then exit 24; fi
if [ "\${PRIVATE_ADMIN_SELFTEST_ECHO:-}" = yes ]; then printf '%s\\n' "$first"; fi
printf '%s\\n' synthetic-ok
`, { mode: 0o700 })
    writeFileSync(secretFile, expected, { mode: 0o600 })
    writeFileSync(identity, 'agent_id: synthetic\n', { mode: 0o600 })
    const secret = readPrivateSecret(secretFile)
    const init = runAdmin(commandSpec(['init', '--binary', binary, '--db', database, '--secret-file', secretFile]), secret)
    const applySpec = commandSpec([
      'agent-apply', '--binary', binary, '--db', database, '--file', identity, '--secret-file', secretFile,
    ])
    const apply = runAdmin(applySpec, secret)
    const saved = process.env.PRIVATE_ADMIN_SELFTEST_ECHO
    process.env.PRIVATE_ADMIN_SELFTEST_ECHO = 'yes'
    const echoProbe = runAdmin(applySpec, secret)
    if (saved === undefined) delete process.env.PRIVATE_ADMIN_SELFTEST_ECHO
    else process.env.PRIVATE_ADMIN_SELFTEST_ECHO = saved
    secret.fill(0)
    if (init.classification !== 'success' || apply.classification !== 'success' ||
        echoProbe.classification !== 'secret_echo_detected' || !echoProbe.echoDetected) throw new Error('self test failed')
    chmodSync(secretFile, 0o644)
    try {
      readPrivateSecret(secretFile)
      throw new Error('permission check failed')
    } catch (error) {
      if (error.message === 'permission check failed') throw error
    }
    return { classification: 'self_test_passed', exitCode: 0, echoDetected: false }
  } finally {
    rmSync(root, { recursive: true, force: true })
  }
}

function main() {
  if (process.argv.length === 3 && process.argv[2] === '--self-test') return selfTest()
  let spec
  try {
    spec = commandSpec(process.argv.slice(2))
  } catch {
    return { classification: 'invalid_arguments', exitCode: 2, echoDetected: false }
  }
  let secret
  try {
    secret = readPrivateSecret(spec.secretFile)
  } catch {
    return { classification: 'private_input_rejected', exitCode: 3, echoDetected: false }
  }
  const result = runAdmin(spec, secret)
  secret.fill(0)
  return result
}

try {
  const result = main()
  report(result.classification, result.exitCode, result.echoDetected)
  process.exitCode = result.classification === 'success' || result.classification === 'self_test_passed' ? 0 : 1
} catch {
  report('helper_failed', 1, false)
  process.exitCode = 1
}
