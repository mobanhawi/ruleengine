const PRESETS = {
  full: {
    environment: "development",
    yaml: `apiVersion: v1
kind: RulesetConfig
metadata:
  name: cel-rulesets-playground
  description: "Interactive CEL Rule Engine Playground"

globals:
  min_age: 18
  max_retries: 5
  allowed_domains:
    - "example.com"
    - "test.org"

rules:
  age_validation:
    name: "Age Validation"
    expression: "user.age >= globals.min_age"

  email_format:
    name: "Email Format Check"
    expression: |
      user.email.matches("^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\\\.[a-zA-Z]{2,}$")

  email_whitelist:
    name: "Domain Whitelist Check"
    extends: email_format
    expression: |
      globals.allowed_domains.exists(domain, user.email.endsWith("@" + domain))

  user_status:
    name: "User Status Check"
    expression: "user.status == 'active' && !user.suspended"

  rate_limiting:
    name: "Rate Limiting"
    expression: "request.attempt <= globals.max_retries"

  user_tier:
    name: "User Tier Check"
    expression: "user.tier == 'premium' || user.tier == 'enterprise'"

  sanctioned_region:
    name: "Sanctioned Region"
    expression: "request.region == 'embargoed'"

rulesets:
  user_registration:
    name: "User Registration (AND)"
    selector: "AND"
    rules:
      - age_validation
      - email_whitelist
      - user_status

  request_throttling:
    name: "Request Throttling (OR)"
    selector: "OR"
    rules:
      - rate_limiting
      - user_tier

  compliance_blocklist:
    name: "Compliance Blocklist (NONE / NOR)"
    selector: "NONE"
    rules:
      - sanctioned_region

  kyc_quorum:
    name: "KYC 2-of-3 Quorum (min_passed)"
    min_passed: 2
    rules:
      - age_validation
      - email_whitelist
      - user_tier

execution_policies:
  collect_all:
    stop_on_failure: false
    max_execution_time: "5s"
  fail_fast:
    stop_on_failure: true
    max_execution_time: "5s"

error_handling:
  execution_policy: "collect_all"
  custom_error_messages:
    age_validation: "user must meet minimum age requirement"
    email_whitelist: "email domain is not on the allowed list"

environments:
  development:
    globals:
      min_age: 13
  production:
    globals:
      min_age: 18
    error_handling:
      execution_policy: "fail_fast"
`,
    json: JSON.stringify(
      {
        user: {
          age: 16,
          email: "alice@example.com",
          status: "active",
          suspended: false,
          tier: "free",
        },
        request: {
          attempt: 2,
          region: "au-syd",
          time: "2026-11-15T10:30:00Z",
        },
      },
      null,
      2
    ),
  },
  none_fail_closed: {
    environment: "production",
    yaml: `apiVersion: v1
kind: RulesetConfig
rules:
  is_blocked:
    name: "Blocked Account Flag"
    expression: "user.blocked == true"

rulesets:
  allow_if_not_blocked:
    name: "Allow Only When Not Blocked (NONE)"
    selector: "NONE"
    rules:
      - is_blocked
`,
    json: JSON.stringify(
      {
        user: {
          name: "bob",
        },
      },
      null,
      2
    ),
  },
  quorum: {
    environment: "production",
    yaml: `apiVersion: v1
kind: RulesetConfig
rules:
  has_passkey:
    name: "Passkey Verified"
    expression: "auth.passkey == true"
  has_totp:
    name: "TOTP Verified"
    expression: "auth.totp == true"
  trusted_ip:
    name: "Trusted Corporate IP"
    expression: "auth.corporate_network == true"

rulesets:
  step_up_mfa:
    name: "Step-Up MFA (Any 2 of 3 Factors)"
    min_passed: 2
    rules:
      - has_passkey
      - has_totp
      - trusted_ip
`,
    json: JSON.stringify(
      {
        auth: {
          passkey: true,
          totp: false,
          corporate_network: true,
        },
      },
      null,
      2
    ),
  },
};

const yamlEditor = document.getElementById("yaml-editor");
const jsonEditor = document.getElementById("json-editor");
const envSelect = document.getElementById("env-select");
const presetSelect = document.getElementById("preset-select");
const resultsContainer = document.getElementById("results-container");
const wasmStatus = document.getElementById("wasm-status");
const runBtn = document.getElementById("run-btn");
const shareBtn = document.getElementById("share-btn");

let activeTab = "visual";
let lastResponse = null;
let reqCounter = 0;
let debounceTimer = null;

const worker = new Worker("worker.js");

worker.onmessage = (e) => {
  const { type, data } = e.data || {};
  if (type === "ready") {
    wasmStatus.textContent = "Wasm Ready";
    wasmStatus.classList.add("ready");
    triggerEvaluation();
    return;
  }
  if (type === "result") {
    lastResponse = data;
    renderOutput();
  }
};

function triggerEvaluation() {
  reqCounter += 1;
  worker.postMessage({
    id: reqCounter,
    type: "evaluate",
    payload: {
      config_yaml: yamlEditor.value,
      environment: envSelect.value,
      input_json: jsonEditor.value,
    },
  });
}

function scheduleEvaluation() {
  clearTimeout(debounceTimer);
  debounceTimer = setTimeout(triggerEvaluation, 180);
}

function escapeHTML(str) {
  return String(str)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
}

function selectorLabel(rs) {
  if (rs && rs.min_passed > 0) {
    const total = Array.isArray(rs.rules) ? rs.rules.length : "?";
    return `MIN_PASSED (${rs.min_passed}/${total})`;
  }
  return (rs && rs.selector) || "AND";
}

function renderOutput() {
  if (!lastResponse) {
    return;
  }
  if (activeTab === "raw") {
    resultsContainer.innerHTML = `<div class="card"><pre class="code-out">${escapeHTML(
      JSON.stringify(lastResponse, null, 2)
    )}</pre></div>`;
    return;
  }
  if (activeTab === "mcp") {
    let parsedInput = {};
    try {
      parsedInput = JSON.parse(jsonEditor.value || "{}");
    } catch (_) {
      parsedInput = {};
    }
    const mcpCall = {
      jsonrpc: "2.0",
      id: 1,
      method: "tools/call",
      params: {
        name: "evaluate_all_rulesets",
        arguments: {
          input: parsedInput,
        },
      },
    };
    resultsContainer.innerHTML = `<div class="card">
      <div class="card-header"><span class="card-title">MCP tools/call Request (ruleengine-mcp)</span></div>
      <pre class="code-out">${escapeHTML(JSON.stringify(mcpCall, null, 2))}</pre>
    </div>`;
    return;
  }

  if (!lastResponse.ok) {
    resultsContainer.innerHTML = `<div class="card">
      <div class="card-header">
        <span class="card-title">Configuration / Evaluation Error</span>
        <span class="pill fail">ERROR</span>
      </div>
      <div class="error-msg">${escapeHTML(lastResponse.error)}</div>
    </div>`;
    return;
  }

  const rsResults = lastResponse.ruleset_results || {};
  const rulesets = lastResponse.rulesets || {};
  const rsNames = Object.keys(rsResults).sort();
  const passedCount = rsNames.filter((k) => rsResults[k].passed).length;

  let html = `<div class="card" style="display:flex;justify-content:space-between;align-items:center">
    <span style="font-size:12.5px;color:var(--muted)">
      Rulesets Passed: <strong style="color:var(--text)">${passedCount} / ${rsNames.length}</strong>
    </span>
    <span class="badge">${lastResponse.duration_ms.toFixed(2)} ms</span>
  </div>`;

  for (const rsID of rsNames) {
    const res = rsResults[rsID];
    const def = rulesets[rsID] || {};
    const statusClass = res.passed ? "pass" : "fail";
    const statusText = res.passed ? "PASSED" : "FAILED";
    const ruleEntries = Object.entries(res.rule_results || {});

    html += `<div class="card">
      <div class="card-header">
        <span class="card-title">${escapeHTML(rsID)}</span>
        <div>
          <span class="pill selector">${escapeHTML(selectorLabel(def))}</span>
          <span class="pill ${statusClass}">${statusText}</span>
        </div>
      </div>
      ${res.error ? `<div class="error-msg">${escapeHTML(res.error)}</div>` : ""}
      <div class="rule-list">
        ${ruleEntries
          .map(([rName, rRes]) => {
            const rPill = rRes.eval_error
              ? `<span class="pill warn">EVAL ERROR</span>`
              : rRes.passed
              ? `<span class="pill pass">PASS</span>`
              : `<span class="pill fail">FAIL</span>`;
            return `<div class="rule-item">
              <div class="rule-row">
                <span style="font-family:var(--font-mono)">${escapeHTML(rName)}</span>
                ${rPill}
              </div>
              ${
                rRes.eval_error
                  ? `<div class="eval-error-msg">eval_error: ${escapeHTML(rRes.eval_error)}</div>`
                  : rRes.error
                  ? `<div class="error-msg">${escapeHTML(rRes.error)}</div>`
                  : ""
              }
            </div>`;
          })
          .join("")}
      </div>
    </div>`;
  }

  resultsContainer.innerHTML = html;
}

function loadPreset(name) {
  const p = PRESETS[name] || PRESETS.full;
  yamlEditor.value = p.yaml;
  jsonEditor.value = p.json;
  envSelect.value = p.environment;
  triggerEvaluation();
}

function loadFromHash() {
  if (!location.hash.startsWith("#state=")) {
    return false;
  }
  try {
    const encoded = location.hash.slice("#state=".length);
    const decoded = JSON.parse(decodeURIComponent(escape(atob(encoded))));
    if (decoded.yaml && decoded.json) {
      yamlEditor.value = decoded.yaml;
      jsonEditor.value = decoded.json;
      envSelect.value = decoded.env || "development";
      return true;
    }
  } catch (_) {
    return false;
  }
  return false;
}

shareBtn.addEventListener("click", async () => {
  const payload = {
    yaml: yamlEditor.value,
    json: jsonEditor.value,
    env: envSelect.value,
  };
  const encoded = btoa(unescape(encodeURIComponent(JSON.stringify(payload))));
  history.replaceState(null, "", `#state=${encoded}`);
  try {
    await navigator.clipboard.writeText(location.href);
    shareBtn.textContent = "Copied URL!";
    setTimeout(() => {
      shareBtn.textContent = "Share Link";
    }, 1500);
  } catch (_) {}
});

presetSelect.addEventListener("change", () => loadPreset(presetSelect.value));
envSelect.addEventListener("change", triggerEvaluation);
runBtn.addEventListener("click", triggerEvaluation);
yamlEditor.addEventListener("input", scheduleEvaluation);
jsonEditor.addEventListener("input", scheduleEvaluation);

document.addEventListener("keydown", (e) => {
  if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
    e.preventDefault();
    triggerEvaluation();
  }
});

for (const [id, tabName] of [
  ["tab-visual", "visual"],
  ["tab-mcp", "mcp"],
  ["tab-raw", "raw"],
]) {
  document.getElementById(id).addEventListener("click", (e) => {
    activeTab = tabName;
    for (const btn of document.querySelectorAll(".tab-btn")) {
      btn.classList.remove("active");
    }
    e.currentTarget.classList.add("active");
    renderOutput();
  });
}

if (!loadFromHash()) {
  loadPreset("full");
}
worker.postMessage({ id: 0, type: "init" });
