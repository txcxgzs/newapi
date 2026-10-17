/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <http://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

/**
 * Locale parity check (run from `web/`, after `node scripts/sync-i18n.mjs`).
 *
 * Asserts, for all seven locale files:
 *   - valid JSON with an identical key set AND key order (sync order);
 *   - `{{placeholder}}` parity against `en.json` for every key;
 *   - inside the New API Pro merchant / market batch, every value differs from
 *     `en` except the deliberately identical brand / model / symbol strings.
 *
 * Reports per-locale counts of translated / intentionally identical / remaining
 * values and exits non-zero when any assertion fails.
 */
import fs from 'node:fs/promises'
import path from 'node:path'

const LOCALES_DIR = path.resolve('src/i18n/locales')
const BASE_LOCALE = 'en'
const LOCALES = ['en', 'zh', 'zh-TW', 'fr', 'ja', 'ru', 'vi']

/** Keys added to `en.json` by the New API Pro merchant / market batch. */
const FEATURE_KEYS = [
  'API Market',
  'All Pools',
  'Last Price',
  '24h Change',
  '24h High',
  '24h Low',
  '24h Calls',
  '24h Volume',
  '24h Revenue',
  '24h Success Rate',
  'High',
  'Low',
  'Live Market',
  'Merchant Portal',
  'Merchants',
  'Become a Merchant',
  'Merchant Management',
  'Merchant Application',
  'Pending Review',
  'Approved',
  'Rejected',
  'Wallet Balance',
  'Profit ratio',
  'My Quotes',
  'Earnings Ledger',
  'Withdrawals',
  'Pool',
  'Input $/1M',
  'Output $/1M',
  'Approve',
  'Reject',
  'Amount (USD)',
  'Available balance',
  'Request Withdrawal',
  'Account',
  'Paid',
  'Payout Account',
  'Withdrawal request submitted',
  'Applications',
  'Review',
  'Site Name',
  'Model Quotes',
  'Submit Application',
  'Suspend',
  'Suspended',
  'Balance (USD)',
  'Mark Paid',
  'Load more',
  'Edit Quote',
  'Quote saved',
  'No quotes yet',
  'No earnings yet',
  'No withdrawals yet',
  'No applications yet',
  'No merchants yet',
  'Review Note',
  'No transaction data yet',
  'Select a model to view its market',
  'Requested quotes',
  'Payment proof (for mark-as-paid)',
  'Transaction ID / screenshot link',
  'Manual quotes override auto-synced prices for this model.',
  'One model per line: model, input $/1M, output $/1M',
  'Application submitted, waiting for review',
  'Please fill in all fields with at least one model quote',
  'Submit your site, API key and model quotes. After approval, your channel joins the pools and starts competing.',
  'Your channel has been created. You now have access to the merchant portal.',
  'Real transaction prices of every model pool, aggregated per minute',
  'Candles reflect blended $/1M-token transaction prices, while the listing ratio comes from merchant asks',
  'Site',
  'Applicant',
  'Pools (comma separated)',
  'Profit ratio (0~1)',
  'Merchant',
  'Interval',
  'Chart failed to render',
  'K-line candlestick chart ({{interval}}), latest close {{close}}',
  'Public live market with per-minute model pool prices.',
  'Require login to view the market',
  'Visitors must authenticate before accessing the market page.',
  'Quote Board',
  'New Quote',
  'Pool Group',
  'Manual',
  'Sync now',
  'Sync completed',
  'Delete quote',
  'Are you sure you want to delete this quote?',
  'Quote deleted',
  'Failed to save quote',
  'Failed to delete quote',
  'Failed to sync quotes',
  'Failed to review application',
  'Failed to update merchant',
  'Failed to review withdrawal',
  'Failed to submit application',
  'Failed to submit withdrawal request',
  "Please fill in Site URL and API Key first",
  "Probe failed, please check the site URL",
  "New API upstreams expose a public pricing API, so rates sync automatically; Sub2API and OpenAI-compatible upstreams have none and list at the platform standard price.",
  "Ratio auto-fetch",
  "Detecting...",
  "Checks your upstream and fetches its model prices once, so rates sync automatically instead of being entered by hand.",
  "Ratio auto-fetch works",
  "Fetched {{total}} model prices from the upstream. After approval the platform re-syncs them on a schedule — no manual upkeep.",
  "Ratio auto-fetch unavailable",
  "Quotes will list at the platform standard price (rate 1.0). Optionally set a discount below, or adjust the rate later on the channel card.",
  "Optional discount rate (fallback)",
  "View my channels in {{pool}}",
  "Click to view my channels",
  "Click to join this pool",
  "Adding a channel submits an expansion application; it goes live after admin approval.",
  "Synced {{count}} quotes",
  "Upstream has no price API to sync from",
  "Sync failed, check the upstream availability",
  "Never synced",
  "Derived",
  "Rates are fetched from your upstream automatically and re-synced on a schedule — no manual upkeep needed.",
  "Probe upstream & pull models",
  "Checks your upstream, tests whether rates can be fetched automatically, and pulls its model list for mapping — no manual model names.",
  "Model mapping",
  "Map each upstream model to the platform model it serves; unmapped models stay off the shelf. Same-name models are mapped automatically.",
  "Duplicate mapping: {{model}} is mapped by multiple upstream models; only the first takes effect.",
  "{{mapped}} of {{total}} mapped",
  "Filter models",
  "Select a platform model",
  "No matching platform model",
  "Platform model for {{model}}",
  "Skipped",
  "Use the filter to see the remaining {{count}} models",
  "Hide manual model list",
  "Couldn't pull the model list? Fill in manually",
  "Duplicate",
  "Last 3 days",
  "Last 7 days",
  "Last 30 days",
  "All time",
  "Total earnings",
  "Daily earnings, last 30 days",
  "Page total",
  "Page {{page}} of {{total}}",
  "Prev",
  "{{count}} records",
  "Search models or channels",
  "{{count}} quotes",
  "Auto-synced quotes follow your upstream on a schedule; derived quotes follow your rate; manual quotes are locked by admins.",
  "Calls",
  "Spend",
  "Channel updated",
  "Leave blank to keep the current key",
  "Edits take effect immediately; site URL or key changes are verified against your upstream before saving.",
  "Edits apply to the current listing; probing pulls the full upstream model list using the saved or newly entered credentials.",
  "Upstream model",
  "Platform model",
  "Remove from list",
  "Providers",
  "Your apps",
  "One gateway, every model",
  "Serving massive AI models",
  "One hub, 100+ premium upstreams",
  "Compatible with OpenAI formats",
  "100+ upstream channels",
  'MyRelay',
  'sk-...',
  'Alipay / USDT / Bank card...',
  'gpt-pro, gpt-plus',
  'gpt-5.6, 2.5, 10\ngpt-6, 4, 32',
]

/**
 * Strings that must stay in English: example brand, key format, model names
 * and the price-format placeholder are protocol/product literals.
 */
const INTENTIONAL_IDENTICAL = new Set([
  'MyRelay',
  'sk-...',
  'gpt-pro, gpt-plus',
  'gpt-5.6, 2.5, 10\ngpt-6, 4, 32',
])

/** English technical loanwords accepted unchanged by a specific locale. */
const LOCALE_IDENTICAL = {
  fr: new Set(['Pool']),
  vi: new Set(['Pool']),
}

/**
 * Pre-existing placeholder deviations outside this batch, verified identical
 * to the committed locale files: the Japanese files replace `{{...}}` with a
 * `__ PH_0 __` marker, and the Vietnamese string wraps "LinuxDO" in a
 * placeholder the source does not have. Left untouched on purpose.
 */
const PREEXISTING_PLACEHOLDER_EXEMPTIONS = new Set([
  'ja|Failed to parse JSON file: {{name}}',
  'ja|Model pull failed: {{msg}}',
  'ja|Parsed {{count}} service account file(s)',
  'vi|Minimum LinuxDO trust level required',
])

const PLACEHOLDER_RE = /\{\{[^{}]+\}\}/g

function placeholdersOf(value) {
  if (typeof value !== 'string') return []
  return [...(value.match(PLACEHOLDER_RE) ?? [])].sort()
}

async function main() {
  const failures = []
  const parsed = {}

  for (const locale of LOCALES) {
    const file = path.join(LOCALES_DIR, `${locale}.json`)
    try {
      const json = JSON.parse(await fs.readFile(file, 'utf8'))
      if (!json || typeof json.translation !== 'object' || json.translation === null) {
        failures.push(`${locale}: missing "translation" object`)
        continue
      }
      parsed[locale] = json.translation
    } catch (error) {
      failures.push(`${locale}: invalid JSON (${error.message})`)
    }
  }

  if (failures.length) {
    for (const failure of failures) console.error(`FAIL ${failure}`)
    process.exitCode = 1
    return
  }

  const baseTrans = parsed[BASE_LOCALE]
  const baseKeys = Object.keys(baseTrans)
  const baseKeySet = new Set(baseKeys)

  let placeholderChecked = 0
  let placeholderExempt = 0
  const report = []

  for (const locale of LOCALES) {
    const trans = parsed[locale]
    const keys = Object.keys(trans)
    const keySet = new Set(keys)

    const missing = baseKeys.filter((key) => !keySet.has(key))
    const extra = keys.filter((key) => !baseKeySet.has(key))
    if (missing.length) {
      failures.push(`${locale}: ${missing.length} missing keys (${missing.slice(0, 3)})`)
    }
    if (extra.length) {
      failures.push(`${locale}: ${extra.length} extra keys (${extra.slice(0, 3)})`)
    }
    if (!missing.length && !extra.length && keys.join('\n') !== baseKeys.join('\n')) {
      failures.push(`${locale}: key order differs from ${BASE_LOCALE}`)
    }

    for (const key of baseKeys) {
      if (!keySet.has(key)) continue
      const expected = placeholdersOf(baseTrans[key])
      const actual = placeholdersOf(trans[key])
      if (actual.join(',') !== expected.join(',')) {
        if (PREEXISTING_PLACEHOLDER_EXEMPTIONS.has(`${locale}|${key}`)) {
          placeholderExempt++
        } else {
          failures.push(
            `${locale}: placeholder mismatch for "${key}" (expected ${expected}, got ${actual})`
          )
        }
      }
      if (expected.length) placeholderChecked++
    }

    if (locale === BASE_LOCALE) {
      report.push({ locale, translated: 0, intentional: 0, remaining: 0 })
      continue
    }

    const accepted = LOCALE_IDENTICAL[locale] ?? new Set()
    let translated = 0
    let intentional = 0
    let remaining = 0

    for (const key of FEATURE_KEYS) {
      if (trans[key] !== baseTrans[key]) {
        translated++
        continue
      }
      if (INTENTIONAL_IDENTICAL.has(key) || accepted.has(key)) {
        intentional++
      } else {
        remaining++
        failures.push(`${locale}: "${key}" is still identical to English`)
      }
    }

    report.push({ locale, translated, intentional, remaining })
  }

  console.log(`locale files: ${LOCALES.length}`)
  console.log(`feature keys in scope: ${FEATURE_KEYS.length}`)
  console.log(`keys with placeholders checked: ${placeholderChecked}`)
  console.log(
    `placeholder mismatches exempted (pre-existing): ${placeholderExempt}`
  )
  console.log('')
  console.log('locale  translated  intentional  remaining')
  for (const row of report) {
    console.log(
      `${row.locale.padEnd(7)} ${String(row.translated).padStart(10)} ${String(
        row.intentional
      ).padStart(12)} ${String(row.remaining).padStart(10)}`
    )
  }
  console.log('')

  if (failures.length) {
    console.error(`FAILED (${failures.length}):`)
    for (const failure of failures) console.error(`  - ${failure}`)
    process.exitCode = 1
    return
  }

  console.log('OK: key sets, key order, placeholders and translations verified.')
}

main().catch((error) => {
  console.error(error)
  process.exitCode = 1
})
