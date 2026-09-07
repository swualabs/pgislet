const $ = (id) => document.getElementById(id)
const editor = $('sql')
const history = []
let connected = false
let busy = false
let resetMode = 'reset'
const presets = {
    select: 'SELECT name, category, price\nFROM products\nORDER BY price DESC;',
    join: 'SELECT c.name AS customer, p.name AS product,\n       o.quantity, p.price * o.quantity AS total\nFROM orders o\nJOIN customers c ON c.id = o.customer_id\nJOIN products p ON p.id = o.product_id\nORDER BY o.id;',
    create: 'CREATE TABLE notes (\n  id integer GENERATED ALWAYS AS IDENTITY PRIMARY KEY,\n  title text NOT NULL,\n  created_at timestamptz DEFAULT now()\n);',
}

function element(tag, text, className) {
    const node = document.createElement(tag)
    if (text !== undefined) node.textContent = text
    if (className) node.className = className
    return node
}

function updateLines() {
    $('line-numbers').textContent = Array.from({ length: editor.value.split('\n').length }, (_, i) => i + 1).join('\n')
    $('line-numbers').scrollTop = editor.scrollTop
}

function setEditor(sql) {
    editor.value = sql
    updateLines()
    editor.focus()
}

function setBusy(value) {
    busy = value
    for (const id of ['run', 'reset', 'seed', 'refresh']) $(id).disabled = value || !connected
    $('reconnect').disabled = value
    $('run').replaceChildren(
        element('span', value ? '◌' : '▶'),
        document.createTextNode(value ? 'Running…' : 'Run SQL'),
    )
}

function sessionInfo(h) {
    $('workspace-id').textContent = h.ID.slice(0, 12) + '…'
    $('workspace-state').textContent = 'Isolated Islet · ' + h.State
}

async function api(path, options = {}) {
    let response
    try {
        response = await fetch('/api/' + path, {
            ...options,
            headers: {
                'X-Pgislet-Request': 'playground',
                'Content-Type': 'application/json',
            },
            credentials: 'same-origin',
        })
    } catch {
        throw {
            error: {
                kind: 'connection',
                message: 'The server connection was lost. If a query was running, check the outcome before retrying.',
            },
        }
    }

    const data = await response.json()
    if (!response.ok) {
        if (response.status === 401) {
            connected = false
            $('reconnect').hidden = false
            $('connection-label').textContent = 'Reconnect required'
            $('connection-dot').classList.remove('ready')
        }
        throw data
    }
    return data
}

function showError(data) {
    const problem = data.error || { message: 'Unable to complete the request.' }
    const lines = [problem.code ? 'SQLSTATE ' + problem.code + ' · ' + problem.message : problem.message]
    if (problem.detail) lines.push(problem.detail)
    if (problem.hint) lines.push('Hint: ' + problem.hint)
    if (problem.position) lines.push('SQL position: ' + problem.position)
    $('error').textContent = lines.join('\n')
    $('error').hidden = false
    $('status').textContent = problem.message
}

function clearError() {
    $('error').hidden = true
    $('error').textContent = ''
}

function empty(title, message) {
    const box = element('div', undefined, 'empty-state')
    box.append(element('span', '⌘', 'empty-symbol'), element('strong', title), element('p', message))
    $('result').replaceChildren(box)
}

function renderResult(result, succeeded) {
    const rows = result.rows || []
    const columns = result.columns || []
    $('row-count').textContent = rows.length
    $('result-meta').textContent = succeeded
        ? '✓  ' + Number(result.durationMs || 0).toFixed(1) + ' ms'
        : 'Operation failed'
    $('result-meta').classList.toggle('success', succeeded)
    $('command').textContent = result.truncated
        ? 'RESULT LIMIT · ROLLBACK'
        : result.commandTag || (succeeded ? 'Done' : 'Failed')

    if (!columns.length) {
        empty(
            succeeded ? 'Execution completed.' : 'Check your query.',
            succeeded
                ? (result.commandTag || 'Done') + ' · Rows affected: ' + result.rowsAffected
                : 'Review the error and update your SQL.',
        )
        return
    }

    if (!rows.length) {
        empty(
            succeeded ? 'No rows returned.' : 'No results to display.',
            succeeded
                ? 'The query completed successfully. Check the conditions or table contents.'
                : 'Review the error details. The operation did not succeed.',
        )
        return
    }

    const table = element('table')
    const caption = element('caption', 'SQL query results', 'sr-only')
    const head = element('thead')
    const header = element('tr')
    header.append(element('th', '#', 'row-number'))
    for (const column of columns) {
        const cell = element('th', column.Name)
        cell.scope = 'col'
        header.append(cell)
    }
    head.append(header)
    const body = element('tbody')
    rows.forEach((row, i) => {
        const tr = element('tr')
        tr.append(element('td', i + 1, 'row-number'))
        for (const value of row) {
            const td = element('td', value === null ? 'NULL' : String(value), value === null ? 'null' : undefined)
            td.title = value === null ? 'SQL NULL' : String(value)
            tr.append(td)
        }
        body.append(tr)
    })
    table.append(caption, head, body)
    $('result').replaceChildren(table)
}

function remember(sql, ok) {
    history.unshift({ sql, ok })
    history.splice(6)
    $('history').replaceChildren(
        ...history.map((item) => {
            const button = element('button', undefined, 'history-item')
            button.append(
                element('span', item.ok ? '✓' : '×', item.ok ? 'ok' : 'bad'),
                element('code', item.sql.replace(/\s+/g, ' ')),
            )
            button.title = item.sql
            button.addEventListener('click', () => setEditor(item.sql))
            return button
        }),
    )
}

async function refreshSchema() {
    const data = await api('schema')
    sessionInfo(data.islet)
    if (!data.tables.length) {
        $('tables').replaceChildren(
            element('p', 'The workspace is empty. Start with CREATE TABLE or restore the samples.', 'muted'),
        )
        return
    }

    $('tables').replaceChildren(
        ...data.tables.map((table) => {
            const group = element('div', undefined, 'table-group')
            const button = element('button', undefined, 'table-button')
            button.append(element('span', '▦', 'table-symbol'), document.createTextNode(table.name))
            button.title = table.name + ': insert a SELECT query'
            button.addEventListener('click', () =>
                setEditor('SELECT *\nFROM "' + table.name.replaceAll('"', '""') + '"\nLIMIT 100;'),
            )
            group.append(button)
            for (const column of table.columns) {
                const row = element('div', undefined, 'column')
                row.append(element('span', column.name), element('span', column.type, 'column-type'))
                group.append(row)
            }
            return group
        }),
    )
}

async function execute() {
    if (busy || !connected) return
    const selection = editor.value.slice(editor.selectionStart, editor.selectionEnd)
    const sql = (selection || editor.value).trim()
    if (!sql) {
        showError({ error: { message: 'Enter SQL to execute.' } })
        editor.focus()
        return
    }

    setBusy(true)
    clearError()
    $('result-meta').textContent = 'Running…'
    try {
        const data = await api('query', {
            method: 'POST',
            body: JSON.stringify({ sql }),
        })
        renderResult(data.result, true)
        remember(sql, true)
        $('status').textContent = 'SQL execution completed'
        try {
            await refreshSchema()
        } catch (err) {
            showError(err)
        }
    } catch (err) {
        renderResult(err.result || {}, false)
        remember(sql, false)
        showError(err)
    } finally {
        setBusy(false)
    }
}

async function connect() {
    setBusy(true)
    clearError()
    try {
        const h = await api('session', { method: 'POST', body: '{}' })
        connected = true
        sessionInfo(h)
        $('reconnect').hidden = true
        $('connection-label').textContent = 'Connected'
        $('connection-dot').classList.add('ready')
        await refreshSchema()
        setBusy(false)
        if (h.State === 'active') await execute()
    } catch (err) {
        $('reconnect').hidden = false
        $('connection-label').textContent = 'Check connection'
        empty('Unable to connect to the workspace.', 'Check the server and PostgreSQL, then reconnect.')
        showError(err)
    } finally {
        setBusy(false)
    }
}

function confirmReset(mode) {
    resetMode = mode
    const seeded = mode === 'seed'
    $('dialog-title').textContent = seeded ? 'Restore sample data?' : 'Clear the workspace?'
    $('dialog-description').textContent = seeded
        ? 'All current tables and data will be deleted and the customers, products, and orders samples will be recreated.'
        : 'All current tables and data will be deleted. Sample data will not be restored automatically.'
    $('dialog-confirm').textContent = seeded ? 'Restore samples' : 'Clear all'
    $('confirm-dialog').returnValue = ''
    $('confirm-dialog').showModal()
}

$('confirm-dialog').addEventListener('close', async () => {
    if ($('confirm-dialog').returnValue !== 'confirm' || busy) return
    setBusy(true)
    clearError()
    try {
        sessionInfo(await api(resetMode, { method: 'POST', body: '{}' }))
        await refreshSchema()
        $('row-count').textContent = '—'
        $('result-meta').textContent = resetMode === 'seed' ? 'Samples restored' : 'Workspace cleared'
        $('command').textContent = 'Workspace reset'
        $('status').textContent = $('result-meta').textContent
        empty(
            resetMode === 'seed' ? 'Sample data is ready.' : 'Your empty workspace is ready.',
            'Run a SQL query to get started.',
        )
        if (resetMode === 'seed') setEditor(presets.select)
    } catch (err) {
        showError(err)
    } finally {
        setBusy(false)
    }
})

editor.addEventListener('input', updateLines)
editor.addEventListener('scroll', () => {
    $('line-numbers').scrollTop = editor.scrollTop
})
editor.addEventListener('keydown', (event) => {
    if ((event.ctrlKey || event.metaKey) && event.key === 'Enter') {
        event.preventDefault()
        execute()
    }
    if (event.key === 'Tab') {
        event.preventDefault()
        editor.setRangeText('  ', editor.selectionStart, editor.selectionEnd, 'end')
        updateLines()
    }
})
for (const button of document.querySelectorAll('[data-preset]'))
    button.addEventListener('click', () => setEditor(presets[button.dataset.preset]))
$('run').addEventListener('click', execute)
$('reset').addEventListener('click', () => confirmReset('reset'))
$('seed').addEventListener('click', () => confirmReset('seed'))
$('reconnect').addEventListener('click', connect)
$('refresh').addEventListener('click', async () => {
    if (busy) return
    setBusy(true)
    clearError()
    try {
        await refreshSchema()
    } catch (err) {
        showError(err)
    } finally {
        setBusy(false)
    }
})
if (/Mac|iPhone|iPad/.test(navigator.platform)) $('shortcut').textContent = '⌘ ↵'
updateLines()
connect()
