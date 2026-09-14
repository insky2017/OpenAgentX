#!/usr/bin/env node
// E1 wrapper/native network-rule verification. It does not run without an
// explicit freeze-time execution gate and never invokes an AGY model.
import { createHash, randomUUID } from 'node:crypto'
import { lstat, mkdir, readFile, writeFile } from 'node:fs/promises'
import { createServer } from 'node:http'
import { isAbsolute, join, resolve } from 'node:path'
import { connect, createServer as createTCPServer } from 'node:net'
import { spawn } from 'node:child_process'

const command = process.argv[2]
const fixtureDir = process.env.OAX_E1_FIXTURE_DIR
const execute = process.env.OAX_E1_ALLOW_EXECUTION === '1'
const wrapperPath = process.env.OAX_E1_WRAPPER
const helperPath = process.env.OAX_E1_MGRAFTCP_BIN
const clientPath = process.env.OAX_E1_CLIENT_BIN
const requestedCases = (process.env.OAX_E1_CASES || '').split(',').map((value) => value.trim()).filter(Boolean)
const targetHost = '127.0.0.2'
const proxyHost = '127.0.0.1'
const totalTimeoutMS = Number(process.env.OAX_E1_TOTAL_TIMEOUT_MS || '90000')
const caseTimeoutMS = Number(process.env.OAX_E1_CASE_TIMEOUT_MS || '15000')
const cleanupTimeoutMS = 5_000
const activeChildren = new Set()
const activeSockets = new Set()

function assert(condition, message) {
  if (!condition) throw failure('assertion_failed', message)
}

function failure(classification, message) {
  const error = new Error(message)
  error.classification = classification
  return error
}

function delay(milliseconds) {
  return new Promise((resolveDelay) => setTimeout(resolveDelay, milliseconds))
}

function privateFixturePath(value) {
  assert(value && isAbsolute(value), 'OAX_E1_FIXTURE_DIR must be an absolute path')
  const path = resolve(value)
  assert(/^openagentx-e1-[A-Za-z0-9._-]+$/.test(path.split('/').at(-1)), 'fixture directory basename must start with openagentx-e1-')
  return path
}

async function executablePath(value, label) {
  assert(value && isAbsolute(value), `${label} must be an absolute path`)
  const info = await lstat(value)
  assert(!info.isSymbolicLink() && info.isFile() && (info.mode & 0o111) !== 0, `${label} must be an executable non-symlink regular file`)
  return resolve(value)
}

async function sha256(path) {
  return createHash('sha256').update(await readFile(path)).digest('hex')
}

async function assertPrivateRegularFile(path, label) {
  const info = await lstat(path)
  assert(!info.isSymbolicLink() && info.isFile() && (info.mode & 0o777) === 0o600, `${label} must be a non-symlink regular file with mode 0600`)
}

function requireExecutionEnvironment() {
  assert(execute, 'set OAX_E1_ALLOW_EXECUTION=1 only after final E1 freeze authorization')
  assert(requestedCases.length > 0, 'OAX_E1_CASES must explicitly select one or more prepared cases')
  assert(Number.isInteger(totalTimeoutMS) && totalTimeoutMS >= 15_000 && totalTimeoutMS <= 5 * 60_000, 'OAX_E1_TOTAL_TIMEOUT_MS must be 15s through 5m')
  assert(Number.isInteger(caseTimeoutMS) && caseTimeoutMS >= 1_000 && caseTimeoutMS < totalTimeoutMS, 'OAX_E1_CASE_TIMEOUT_MS must be positive and below the total timeout')
}

function trackSocket(socket) {
  activeSockets.add(socket)
  socket.once('close', () => activeSockets.delete(socket))
  socket.once('error', () => socket.destroy())
  return socket
}

function listen(server, host) {
  return new Promise((resolveListen, reject) => {
    server.once('error', reject)
    server.listen({ host, port: 0, exclusive: true }, () => {
      server.off('error', reject)
      const address = server.address()
      if (!address || typeof address === 'string') return reject(new Error('listener did not return a TCP address'))
      resolveListen(address.port)
    })
  })
}

async function closeServerBounded(server, label) {
  if (!server?.listening) return
  const closed = new Promise((resolveClose, rejectClose) => server.close((error) => error ? rejectClose(error) : resolveClose()))
  const result = await Promise.race([closed.then(() => 'closed'), delay(cleanupTimeoutMS).then(() => 'timeout')])
  if (result !== 'closed') throw failure('cleanup_listener_timeout', `${label} did not close before cleanup timeout`)
}

async function assertPrivateDirectory(path, label) {
  const info = await lstat(path)
  assert(!info.isSymbolicLink() && info.isDirectory() && (info.mode & 0o777) === 0o700, `${label} must be a non-symlink directory with mode 0700`)
}

function parseConnectTarget(request) {
  const first = request.split('\r\n', 1)[0]
  const match = /^CONNECT ([^ ]+) HTTP\/1\.[01]$/.exec(first)
  if (!match) return null
  const separator = match[1].lastIndexOf(':')
  if (separator < 1) return null
  return { host: match[1].slice(0, separator), port: Number(match[1].slice(separator + 1)) }
}

function startRestrictedProxy(targetPort) {
  const state = { target_connections: 0 }
  const server = createTCPServer((socket) => {
    trackSocket(socket)
    let buffer = Buffer.alloc(0)
    let phase = 'detect'
    const handshakeTimeout = setTimeout(() => socket.destroy(), 3_000)
    const reject = () => {
      clearTimeout(handshakeTimeout)
      socket.destroy()
    }
    const relay = (host, port, success, remainder) => {
      if (host !== targetHost || port !== targetPort) return reject()
      clearTimeout(handshakeTimeout)
      phase = 'relaying'
      socket.pause()
      socket.off('data', onData)
      state.target_connections += 1
      const upstream = trackSocket(connect({ host: targetHost, port: targetPort }))
      upstream.once('connect', () => {
        success()
        if (remainder.length > 0) upstream.write(remainder)
        socket.pipe(upstream)
        upstream.pipe(socket)
        socket.resume()
      })
    }
    const onData = (chunk) => {
      buffer = Buffer.concat([buffer, chunk])
      if (buffer.length > 4 * 1024) return reject()
      if (phase === 'detect') {
        if (buffer.subarray(0, 8).toString('ascii') === 'CONNECT ') phase = 'connect'
        else if (buffer.length >= 2 && buffer[0] === 0x05) phase = 'socks-greeting'
        else if (buffer.length >= 8) return reject()
      }
      if (phase === 'connect') {
        const end = buffer.indexOf('\r\n\r\n')
        if (end < 0) return
        const target = parseConnectTarget(buffer.subarray(0, end + 4).toString('ascii'))
        const remainder = buffer.subarray(end + 4)
        return target ? relay(target.host, target.port, () => socket.write('HTTP/1.1 200 Connection Established\r\n\r\n'), remainder) : reject()
      }
      if (phase === 'socks-greeting') {
        const size = 2 + buffer[1]
        if (buffer.length < size) return
        if (!buffer.subarray(2, size).includes(0x00)) return reject()
        socket.write(Buffer.from([0x05, 0x00]))
        buffer = buffer.subarray(size)
        phase = 'socks-request'
      }
      if (phase === 'socks-request') {
        if (buffer.length < 10 || buffer[0] !== 0x05 || buffer[1] !== 0x01 || buffer[3] !== 0x01) return
        const host = `${buffer[4]}.${buffer[5]}.${buffer[6]}.${buffer[7]}`
        const port = buffer.readUInt16BE(8)
        const remainder = buffer.subarray(10)
        return relay(host, port, () => socket.write(Buffer.from([0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0])), remainder)
      }
    }
    socket.on('data', onData)
    socket.once('close', () => clearTimeout(handshakeTimeout))
  })
  return { server, state }
}

function startMarkerTarget(marker) {
  const server = createServer((request, response) => {
    if (request.method !== 'GET' || request.url !== '/e1-marker') {
      response.writeHead(404).end()
      return
    }
    response.writeHead(200, { 'content-type': 'text/plain', 'cache-control': 'no-store' })
    response.end(marker)
  })
  server.on('connection', trackSocket)
  return server
}

function signalProcessGroup(child, signal) {
  try {
    if (process.platform !== 'win32' && child.pid) process.kill(-child.pid, signal)
    else child.kill(signal)
  } catch (error) {
    if (error.code !== 'ESRCH') throw error
  }
}

async function terminateChild(entry) {
  if (entry.closedResult) return
  signalProcessGroup(entry.child, 'SIGTERM')
  if (await Promise.race([entry.closed.then(() => true), delay(cleanupTimeoutMS).then(() => false)])) return
  signalProcessGroup(entry.child, 'SIGKILL')
  if (await Promise.race([entry.closed.then(() => true), delay(cleanupTimeoutMS).then(() => false)])) return
  throw failure('cleanup_process_timeout', 'wrapper process group did not exit after TERM/KILL')
}

async function terminateAllChildren() {
  const results = await Promise.allSettled([...activeChildren].map((entry) => terminateChild(entry)))
  const failed = results.find((result) => result.status === 'rejected')
  if (failed) throw failed.reason
}

async function runWrapped({ wrapper, helper, client, config, mode, blackip, whitelist, targetURL, totalState }) {
  const env = {
    PATH: '/usr/bin:/bin',
    AGY_GRAFT_REAL_BIN: client,
    AGY_GRAFT_MGRAFTCP_BIN: helper,
    AGY_GRAFT_CONFIG: config,
    AGY_GRAFT_SELECT_PROXY_MODE: mode,
    ...(mode === 'only_socks5' ? { AGY_GRAFT_IPV4_ONLY: whitelist ? '1' : '0' } : {}),
    ...(blackip ? { AGY_GRAFT_BLACKIP_FILE: blackip } : {}),
    ...(whitelist ? { AGY_GRAFT_IPV4_ONLY_FILE: whitelist } : {}),
  }
  const curlArgs = ['--disable', '--silent', '--show-error', '--fail', '--noproxy', '*', '--connect-timeout', '5', '--max-time', '10', '--proto', '=http', '--proto-redir', '=http', '--output', '-', targetURL]
  const child = spawn(wrapper, curlArgs, { env, stdio: ['ignore', 'pipe', 'pipe'], detached: process.platform !== 'win32' })
  let resolveClosed
  const entry = { child, closed: new Promise((resolveClose) => { resolveClosed = resolveClose }), closedResult: null }
  activeChildren.add(entry)
  let stdout = ''
  let outputBytes = 0
  const countOutput = (chunk, save) => {
    outputBytes += Buffer.byteLength(chunk)
    if (outputBytes > 16 * 1024) void terminateChild(entry)
    else if (save) stdout += chunk
  }
  child.stdout.setEncoding('utf8')
  child.stderr.setEncoding('utf8')
  child.stdout.on('data', (chunk) => countOutput(chunk, true))
  child.stderr.on('data', (chunk) => countOutput(chunk, false))
  child.once('error', (error) => {
    entry.closedResult = { spawn_error: error }
    activeChildren.delete(entry)
    resolveClosed(entry.closedResult)
  })
  child.once('close', (code, signal) => {
    entry.closedResult = { code, signal }
    activeChildren.delete(entry)
    resolveClosed(entry.closedResult)
  })
  const outcome = await Promise.race([entry.closed, delay(caseTimeoutMS).then(() => ({ timeout: true }))])
  if (outcome.timeout) {
    await terminateChild(entry)
    throw failure(totalState.expired ? 'total_timeout' : 'case_timeout', 'wrapper case exceeded its timeout')
  }
  if (totalState.expired) throw failure('total_timeout', 'E1 total timeout elapsed')
  if (outcome.spawn_error) throw failure('wrapper_spawn_error', 'wrapper process could not start')
  if (outcome.signal) throw failure('wrapper_signal_exit', `wrapper process group exited by ${outcome.signal}`)
  if (outcome.code !== 0) throw failure('wrapper_nonzero_exit', `wrapper process group exited with ${outcome.code}`)
  return stdout
}

async function run() {
  requireExecutionEnvironment()
  const root = privateFixturePath(fixtureDir)
  const [wrapper, helper, client] = await Promise.all([
    executablePath(wrapperPath, 'OAX_E1_WRAPPER'), executablePath(helperPath, 'OAX_E1_MGRAFTCP_BIN'), executablePath(clientPath, 'OAX_E1_CLIENT_BIN'),
  ])
  const totalState = { expired: false }
  const deadline = setTimeout(() => {
    totalState.expired = true
    void terminateAllChildren().catch(() => {})
  }, totalTimeoutMS)
  let targetServer
  let proxy
  let primaryFailure
  try {
    await mkdir(root, { mode: 0o700 })
    await assertPrivateDirectory(root, 'fixture directory')
    const marker = `e1-${randomUUID()}-${randomUUID()}`
    targetServer = startMarkerTarget(marker)
    const targetPort = await listen(targetServer, targetHost)
    proxy = startRestrictedProxy(targetPort)
    const proxyPort = await listen(proxy.server, proxyHost)
    const config = join(root, 'agy-graft.conf')
    const httpConfig = join(root, 'agy-graft.http.conf')
    const blackTarget = join(root, 'black-target.txt')
    const blackOther = join(root, 'black-other.txt')
    const whiteTarget = join(root, 'white-target.txt')
    const whiteOther = join(root, 'white-other.txt')
    await writeFile(config, `select_proxy_mode = only_socks5\nsocks5 = ${proxyHost}:${proxyPort}\n`, { mode: 0o600 })
    await writeFile(httpConfig, `select_proxy_mode = only_http_proxy\nhttp_proxy = ${proxyHost}:${proxyPort}\n`, { mode: 0o600 })
    await Promise.all([
      writeFile(blackTarget, `${targetHost}\n`, { mode: 0o600 }), writeFile(blackOther, '127.0.0.3\n', { mode: 0o600 }),
      writeFile(whiteTarget, `${targetHost}\n`, { mode: 0o600 }), writeFile(whiteOther, '127.0.0.3\n', { mode: 0o600 }),
    ])
    await Promise.all([config, httpConfig, blackTarget, blackOther, whiteTarget, whiteOther].map((path) => assertPrivateRegularFile(path, 'fixture configuration')))
    const targetURL = `http://${targetHost}:${targetPort}/e1-marker`
    const preparedCases = [
      { name: 'no_blackip', mode: 'only_socks5', config, expected_proxy: 'increase' },
      { name: 'black_target', mode: 'only_socks5', config, blackip: blackTarget, expected_proxy: 'zero' },
      { name: 'overlap_black_precedes_white', mode: 'only_socks5', config, blackip: blackTarget, whitelist: whiteTarget, expected_proxy: 'zero' },
      { name: 'whitelist_target_nonoverlap_black', mode: 'only_socks5', config, blackip: blackOther, whitelist: whiteTarget, expected_proxy: 'increase' },
      { name: 'black_target_nonoverlap_white', mode: 'only_socks5', config, blackip: blackTarget, whitelist: whiteOther, expected_proxy: 'zero' },
      { name: 'http_config_host_port', mode: 'only_http_proxy', config: httpConfig, expected_proxy: 'increase' },
    ]
    const requested = new Set(requestedCases)
    const cases = preparedCases.filter((test) => requested.has(test.name))
    assert(cases.length === requested.size, 'OAX_E1_CASES contains an unknown prepared case')
    const results = []
    for (const test of cases) {
      assert(!totalState.expired, 'E1 total timeout elapsed')
      proxy.state.target_connections = 0
      const output = await runWrapped({ wrapper, helper, client, config: test.config, mode: test.mode, blackip: test.blackip, whitelist: test.whitelist, targetURL, totalState })
      assert(output.trim() === marker, `${test.name} did not return the controlled target marker`)
      const proxyConnections = proxy.state.target_connections
      if (test.expected_proxy === 'increase') assert(proxyConnections > 0, `${test.name} did not reach the controlled proxy`)
      else assert(proxyConnections === 0, `${test.name} reached the controlled proxy despite a direct-rule expectation`)
      results.push({ case: test.name, mode: test.mode, marker_match: true, proxy_connections: proxyConnections })
    }
    console.log(JSON.stringify({
      e1_native_rules: 'complete', target_host: targetHost, proxy_host: proxyHost,
      wrapper_sha256: await sha256(wrapper), helper_sha256: await sha256(helper), client_sha256: await sha256(client), results,
    }))
  } catch (error) {
    primaryFailure = error
    throw error
  } finally {
    clearTimeout(deadline)
    for (const socket of activeSockets) socket.destroy()
    const cleanup = await Promise.allSettled([
      terminateAllChildren(),
      closeServerBounded(proxy?.server, 'restricted proxy'),
      closeServerBounded(targetServer, 'marker target'),
    ])
    const failed = cleanup.find((result) => result.status === 'rejected')
    if (failed && primaryFailure) primaryFailure.cleanupClassification = 'cleanup_failed'
    if (failed && !primaryFailure) throw failure('cleanup_failed', 'E1 cleanup did not complete')
  }
}

async function main() {
  try {
    if (command === 'run') await run()
    else throw failure('usage', 'usage: verify-e1-network.mjs run')
  } catch (error) {
    console.error(JSON.stringify({ e1_native_rules: 'failed', classification: error.classification || 'internal_error', cleanup: error.cleanupClassification || 'complete' }))
    process.exitCode = 1
  }
}

await main()
