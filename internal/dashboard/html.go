package dashboard

const dashboardHTML2 = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>LlamaWrapper Dashboard</title>
<style>
:root{--bg:#0f172a;--surface:#1e293b;--border:#334155;--text:#e2e8f0;--text-dim:#94a3b8;--text-muted:#64748b;--accent:#3b82f6;--green:#22c55e;--yellow:#eab308;--red:#ef4444;--orange:#f97316;--purple:#a855f7}
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;background:var(--bg);color:var(--text);min-height:100vh;display:flex;flex-direction:column}
.hdr{background:var(--surface);border-bottom:1px solid var(--border);padding:10px 20px;display:flex;align-items:center;gap:10px;position:sticky;top:0;z-index:100}
.hdr h1{font-size:16px;font-weight:700;color:#f8fafc;flex:1}
.hdr .dot{width:8px;height:8px;border-radius:50%;background:var(--green);animation:pulse 2s infinite}
@keyframes pulse{0%,100%{opacity:1}50%{opacity:.4}}
.hdr .uptime{font-size:11px;color:var(--text-muted)}
.alerts{padding:0 20px}
.alert{padding:8px 14px;border-radius:6px;font-size:12px;margin-top:6px;display:flex;align-items:center;gap:6px}
.alert.warn{background:#713f1240;border:1px solid #92400e;color:#fbbf24}
.alert.error{background:#7f1d1d40;border:1px solid #991b1b;color:#f87171}
.alert.ok{background:#064e3b40;border:1px solid #065f46;color:#34d399}
.tabs{display:flex;gap:0;padding:0 20px;margin-top:8px;border-bottom:1px solid var(--border);overflow-x:auto;flex-shrink:0}
.tab{padding:8px 16px;font-size:12px;font-weight:500;color:var(--text-muted);cursor:pointer;border-bottom:2px solid transparent;transition:all .2s;white-space:nowrap}
.tab:hover{color:var(--text)}.tab.active{color:var(--accent);border-bottom-color:var(--accent)}
.tc{display:none;padding:16px 20px;max-width:1400px;margin:0 auto;width:100%}.tc.active{display:block}
.stats{display:grid;grid-template-columns:repeat(auto-fit,minmax(140px,1fr));gap:10px;margin-bottom:16px}
.sc{background:var(--surface);border:1px solid var(--border);border-radius:8px;padding:12px}
.sc .lb{font-size:10px;color:var(--text-muted);text-transform:uppercase;letter-spacing:.5px}
.sc .vl{font-size:22px;font-weight:700;color:#f8fafc}.sc .sb{font-size:10px;color:var(--text-muted);margin-top:2px}
.card{background:var(--surface);border:1px solid var(--border);border-radius:8px;padding:14px;margin-bottom:12px}
.card h3{font-size:13px;font-weight:600;margin-bottom:10px;color:var(--text-dim);display:flex;align-items:center;justify-content:space-between}
table{width:100%;border-collapse:collapse}
th{text-align:left;padding:6px 8px;font-size:10px;color:var(--text-muted);text-transform:uppercase;letter-spacing:.5px;border-bottom:1px solid var(--border)}
td{padding:6px 8px;border-bottom:1px solid var(--border);font-size:12px}
tr:hover{background:#ffffff06}.clickable{cursor:pointer}.clickable:hover{background:#ffffff0d}
.badge{display:inline-block;padding:2px 7px;border-radius:9999px;font-size:10px;font-weight:600}
.badge.ready{background:#064e3b;color:#34d399}.badge.starting{background:#713f12;color:#fbbf24}
.badge.failed{background:#7f1d1d;color:#f87171}.badge.stopped{background:#1e293b;color:var(--text-muted)}
.badge.stream{background:#1e3a5f;color:#60a5fa}.badge.cached{background:#064e3b;color:#34d399}
.badge.error{background:#7f1d1d;color:#f87171}.badge.info{background:#1e3a5f;color:#60a5fa}
.badge.warn{background:#713f12;color:#fbbf24}
.btn{padding:5px 12px;border-radius:5px;font-size:11px;font-weight:600;border:none;cursor:pointer;transition:all .15s}
.btn-p{background:var(--accent);color:white}.btn-p:hover{background:#2563eb}
.btn-d{background:#dc2626;color:white}.btn-d:hover{background:#b91c1c}
.btn-s{background:#475569;color:white}.btn-s:hover{background:#64748b}
.btn-sm{padding:3px 8px;font-size:10px}.btn:disabled{opacity:.5;cursor:not-allowed}
.gpu-bar{width:100%;height:6px;background:#334155;border-radius:3px;overflow:hidden;margin-top:4px}
.gpu-fill{height:100%;border-radius:3px;transition:width .5s}
.mg{display:grid;grid-template-columns:repeat(auto-fit,minmax(280px,1fr));gap:10px}
.mc{background:var(--surface);border:1px solid var(--border);border-radius:8px;padding:14px}
.mc h3{font-size:13px;font-weight:600;margin-bottom:4px;display:flex;align-items:center;gap:6px}
.mc .al{font-size:10px;color:var(--text-muted);margin-bottom:6px}
.mc .ms{display:flex;justify-content:space-between;font-size:11px;padding:3px 0;border-bottom:1px solid #ffffff08}
.mc .ms .k{color:var(--text-dim)}.mc .ms .v{color:#f8fafc;font-weight:500}
.mc .acts{margin-top:8px;display:flex;gap:4px;flex-wrap:wrap}
.chart-c{position:relative;height:160px;margin:6px 0}
canvas{width:100%!important;height:100%!important}
.cr{display:grid;grid-template-columns:1fr 1fr;gap:10px}
@media(max-width:800px){.cr{grid-template-columns:1fr}}
.modal-o{display:none;position:fixed;top:0;left:0;right:0;bottom:0;background:#00000080;z-index:200;align-items:center;justify-content:center}
.modal-o.active{display:flex}
.modal{background:var(--surface);border:1px solid var(--border);border-radius:10px;max-width:700px;width:90%;max-height:80vh;overflow-y:auto;padding:20px}
.modal h2{font-size:15px;font-weight:600;margin-bottom:14px;display:flex;align-items:center;justify-content:space-between}
.modal .close{cursor:pointer;color:var(--text-muted);font-size:18px}
.modal .fld{margin-bottom:10px}.modal .fld .fl{font-size:10px;color:var(--text-muted);text-transform:uppercase;margin-bottom:2px}
.modal .fld .fv{font-size:12px;color:var(--text);word-break:break-all}
.modal .fld pre{background:var(--bg);border:1px solid var(--border);border-radius:5px;padding:8px;font-size:11px;max-height:150px;overflow-y:auto;white-space:pre-wrap;word-break:break-word}
.ig{display:grid;grid-template-columns:repeat(auto-fit,minmax(200px,1fr));gap:10px}
.ii{background:var(--surface);border:1px solid var(--border);border-radius:6px;padding:10px}
.ii .il{font-size:10px;color:var(--text-muted);text-transform:uppercase;margin-bottom:2px}.ii .iv{font-size:13px;font-weight:500}
.lf{color:var(--green)}.lm{color:var(--yellow)}.ls{color:var(--orange)}.lb2{color:var(--red)}
textarea{width:100%;background:var(--bg);color:var(--text);border:1px solid var(--border);border-radius:6px;padding:8px;font-family:monospace;font-size:11px;resize:vertical}
select,input[type=text],input[type=number]{background:var(--bg);color:var(--text);border:1px solid var(--border);border-radius:5px;padding:4px 8px;font-size:11px}
.wf{display:flex;gap:6px;align-items:center;flex-wrap:wrap;margin-bottom:8px}
.sla-meter{height:12px;border-radius:6px;overflow:hidden;background:#334155;margin-top:4px}
.sla-fill{height:100%;border-radius:6px;transition:width .5s}
::-webkit-scrollbar{width:5px}::-webkit-scrollbar-track{background:var(--bg)}::-webkit-scrollbar-thumb{background:var(--border);border-radius:3px}
.chat-wrap{display:flex;flex-direction:column;height:calc(100vh - 120px);max-width:900px;margin:0 auto;width:100%}
.chat-header{display:flex;gap:10px;align-items:center;padding:10px 0;flex-wrap:wrap}
.chat-header select,.chat-header input{background:var(--bg);color:var(--text);border:1px solid var(--border);border-radius:5px;padding:5px 10px;font-size:12px}
.chat-header label{font-size:11px;color:var(--text-muted)}
.chat-messages{flex:1;overflow-y:auto;padding:12px 0;display:flex;flex-direction:column;gap:12px}
.chat-msg{display:flex;gap:10px;max-width:85%}
.chat-msg.user{align-self:flex-end;flex-direction:row-reverse}
.chat-msg .avatar{width:32px;height:32px;border-radius:50%;display:flex;align-items:center;justify-content:center;font-size:14px;flex-shrink:0}
.chat-msg.user .avatar{background:#3b82f6;color:white}
.chat-msg.assistant .avatar{background:#8b5cf6;color:white}
.chat-msg .bubble{background:var(--surface);border:1px solid var(--border);border-radius:12px;padding:10px 14px;font-size:13px;line-height:1.6;white-space:pre-wrap;word-break:break-word;min-width:40px}
.chat-msg.user .bubble{background:#1e3a5f;border-color:#2563eb40}
.chat-msg .bubble img{max-width:300px;max-height:200px;border-radius:8px;margin:6px 0;display:block}
.chat-msg .bubble code{background:var(--bg);padding:1px 5px;border-radius:3px;font-size:12px}
.chat-msg .bubble pre{background:var(--bg);border:1px solid var(--border);border-radius:6px;padding:10px;margin:8px 0;overflow-x:auto;font-size:12px;line-height:1.4}
.chat-msg .bubble pre code{background:none;padding:0}
.chat-msg .meta{font-size:10px;color:var(--text-muted);margin-top:4px}
.chat-msg .bubble .thinking{color:var(--text-muted);font-style:italic;border-left:2px solid var(--border);padding-left:8px;margin:4px 0}
.chat-input-area{border-top:1px solid var(--border);padding:12px 0}
.chat-attachments{display:flex;gap:8px;flex-wrap:wrap;margin-bottom:8px}
.chat-attachment{position:relative;border-radius:8px;overflow:hidden;border:1px solid var(--border)}
.chat-attachment img{height:60px;display:block}
.chat-attachment .remove{position:absolute;top:2px;right:2px;width:18px;height:18px;border-radius:50%;background:#00000099;color:white;font-size:12px;line-height:18px;text-align:center;cursor:pointer}
.chat-input-row{display:flex;gap:8px;align-items:flex-end}
.chat-input-row textarea{flex:1;background:var(--bg);color:var(--text);border:1px solid var(--border);border-radius:10px;padding:10px 14px;font-size:13px;font-family:inherit;resize:none;min-height:44px;max-height:200px;line-height:1.5}
.chat-input-row textarea:focus{outline:none;border-color:var(--accent)}
.chat-input-row .actions{display:flex;gap:4px;flex-shrink:0;padding-bottom:4px}
.chat-input-row .actions button{width:36px;height:36px;border-radius:8px;border:none;cursor:pointer;display:flex;align-items:center;justify-content:center;transition:all .15s}
.chat-btn-attach{background:var(--surface);color:var(--text-dim)}
.chat-btn-attach:hover{background:var(--border)}
.chat-btn-send{background:var(--accent);color:white}
.chat-btn-send:hover{background:#2563eb}
.chat-btn-send:disabled{opacity:.4;cursor:not-allowed}
.chat-btn-stop{background:var(--red);color:white}
.chat-drop-zone{border:2px dashed var(--accent);border-radius:12px;padding:40px;text-align:center;color:var(--accent);font-size:14px;display:none;margin-bottom:8px}
.chat-drop-zone.active{display:block}
.typing-dot{display:inline-block;width:6px;height:6px;border-radius:50%;background:var(--text-muted);margin:0 2px;animation:tdot 1.4s infinite}
.typing-dot:nth-child(2){animation-delay:.2s}.typing-dot:nth-child(3){animation-delay:.4s}
@keyframes tdot{0%,80%,100%{opacity:.3}40%{opacity:1}}
.chat-empty{display:flex;flex-direction:column;align-items:center;justify-content:center;flex:1;color:var(--text-muted);gap:12px}
.chat-empty svg{width:48px;height:48px;opacity:.3}
.chat-empty p{font-size:14px}
.chat-clear{font-size:11px;color:var(--text-muted);cursor:pointer;text-decoration:underline}
.chat-clear:hover{color:var(--text)}
</style>
</head>
<body>
<div class="hdr">
<div class="dot" id="dot"></div>
<h1>LlamaWrapper Dashboard</h1>
<span class="uptime" id="uptime"></span>
</div>
<div class="alerts" id="alerts"></div>
<div class="tabs" id="tabs">
<div class="tab active" data-t="overview">Overview</div>
<div class="tab" data-t="models">Models</div>
<div class="tab" data-t="requests">Requests</div>
<div class="tab" data-t="charts">Charts</div>
<div class="tab" data-t="compare">Compare</div>
<div class="tab" data-t="gpu">GPU</div>
<div class="tab" data-t="keys">API Keys</div>
<div class="tab" data-t="events">Events</div>
<div class="tab" data-t="audit">Audit</div>
<div class="tab" data-t="health">Health</div>
<div class="tab" data-t="sla">SLA</div>
<div class="tab" data-t="config">Config</div>
<div class="tab" data-t="chat">Chat</div>
<div class="tab" data-t="system">System</div>
</div>
<!-- Overview -->
<div class="tc active" id="t-overview">
<div class="stats" id="stats"></div>
<div class="card"><h3>Backends<span style="font-size:10px;color:var(--text-muted)">live</span></h3>
<table><thead><tr><th>Model</th><th>Port</th><th>State</th><th>Active</th><th>Last Used</th><th>Actions</th></tr></thead><tbody id="backends"></tbody></table></div>
<div class="card" style="max-height:280px;overflow-y:auto"><h3>Recent Requests<span style="font-size:10px;color:var(--text-muted)" id="sse-status">polling</span></h3>
<table><thead><tr><th>Time</th><th>Model</th><th>Endpoint</th><th>Latency</th><th>Tokens</th><th>Status</th></tr></thead><tbody id="recent-reqs"></tbody></table></div>
</div>
<!-- Models -->
<div class="tc" id="t-models"><div class="mg" id="model-cards"></div></div>
<!-- Requests -->
<div class="tc" id="t-requests"><div class="card">
<div class="wf"><h3 style="margin:0;flex:1">Request History</h3>
<select id="rf"><option value="all">All</option><option value="errors">Errors</option><option value="slow">Slow>2s</option><option value="cached">Cached</option></select>
<a class="btn btn-s btn-sm" href="/dashboard/api/export/requests" target="_blank">Export CSV</a></div>
<table><thead><tr><th>Time</th><th>ID</th><th>Model</th><th>Endpoint</th><th>Latency</th><th>Tokens</th><th>Status</th><th>Source</th></tr></thead><tbody id="all-reqs"></tbody></table></div></div>
<!-- Charts -->
<div class="tc" id="t-charts">
<div class="cr">
<div class="card"><h3>Throughput (req/s)</h3><div class="chart-c"><canvas id="c-rps"></canvas></div></div>
<div class="card"><h3>Tokens/sec</h3><div class="chart-c"><canvas id="c-tps"></canvas></div></div>
</div><div class="cr">
<div class="card"><h3>Avg Latency (ms)</h3><div class="chart-c"><canvas id="c-lat"></canvas></div></div>
<div class="card"><h3>GPU Memory %</h3><div class="chart-c"><canvas id="c-gpu"></canvas></div></div>
</div>
<div class="card"><h3>Hourly Volume<a class="btn btn-s btn-sm" href="/dashboard/api/export/timeseries" target="_blank">Export CSV</a></h3><div class="chart-c" style="height:200px"><canvas id="c-hourly"></canvas></div></div>
</div>
<!-- Compare -->
<div class="tc" id="t-compare"><div class="card"><h3>Model Comparison</h3><table><thead><tr><th>Model</th><th>Requests</th><th>Avg (ms)</th><th>P50</th><th>P95</th><th>P99</th></tr></thead><tbody id="cmp-table"></tbody></table></div></div>
<!-- GPU -->
<div class="tc" id="t-gpu"><div id="gpu-cards"></div>
<div class="card"><h3>VRAM Estimates</h3><table><thead><tr><th>Model</th><th>File Size</th><th>Est VRAM</th><th>GPU Layers</th><th>Context</th><th>Can Fit</th></tr></thead><tbody id="vram-table"></tbody></table></div>
<div class="card"><h3>Disk Usage</h3><table><thead><tr><th>Model</th><th>Path</th><th>Size (MB)</th><th>Exists</th></tr></thead><tbody id="disk-table"></tbody></table></div>
</div>
<!-- Keys -->
<div class="tc" id="t-keys"><div class="card"><h3>API Key Usage<a class="btn btn-s btn-sm" href="/dashboard/api/export/keys" target="_blank">Export CSV</a></h3>
<table><thead><tr><th>Key</th><th>Requests</th><th>Tokens</th><th>Errors</th><th>Last Request</th></tr></thead><tbody id="key-table"></tbody></table></div>
<div class="card"><h3>Key Management</h3><div class="wf"><button class="btn btn-p btn-sm" onclick="createKey()">Generate New Key</button></div><div id="key-mgmt"></div></div></div>
<!-- Events -->
<div class="tc" id="t-events"><div class="card"><h3>System Events</h3>
<table><thead><tr><th>Time</th><th>Level</th><th>Source</th><th>Model</th><th>Message</th></tr></thead><tbody id="event-table"></tbody></table></div>
<div class="card"><h3>Model Timeline</h3><table><thead><tr><th>Time</th><th>Model</th><th>Event</th><th>Detail</th></tr></thead><tbody id="timeline-table"></tbody></table></div></div>
<!-- Audit -->
<div class="tc" id="t-audit"><div class="card"><h3>Audit Log</h3>
<table><thead><tr><th>Time</th><th>Action</th><th>Actor</th><th>Target</th><th>Detail</th></tr></thead><tbody id="audit-table"></tbody></table></div></div>
<!-- Health -->
<div class="tc" id="t-health"><div class="card"><h3>Health Check History</h3>
<table><thead><tr><th>Time</th><th>Model</th><th>Port</th><th>OK</th><th>Latency</th></tr></thead><tbody id="health-table"></tbody></table></div></div>
<!-- SLA -->
<div class="tc" id="t-sla"><div class="ig" id="sla-cards"></div>
<div class="card" style="margin-top:12px"><h3>Scheduled Actions</h3><div class="wf" style="margin-top:8px">
<select id="sched-model"></select><input type="number" id="sched-min" placeholder="Minutes" style="width:80px" value="30">
<button class="btn btn-p btn-sm" onclick="addSchedule()">Add Unload-Idle Rule</button></div>
<table><thead><tr><th>ID</th><th>Type</th><th>Model</th><th>After</th><th>Action</th></tr></thead><tbody id="sched-table"></tbody></table></div></div>
<!-- Config -->
<div class="tc" id="t-config">
<div class="card"><h3>Feature Toggles</h3><div class="ig" id="toggles"></div></div>
<div class="card"><h3>Config Editor<span><button class="btn btn-p btn-sm" onclick="saveConfig()">Save</button> <button class="btn btn-s btn-sm" onclick="reloadConfig()">Reload</button></span></h3>
<textarea id="cfg-editor" rows="20"></textarea></div>
<div class="card"><h3>Add Model</h3><div class="wf">
<input type="text" id="am-name" placeholder="Model name"><input type="text" id="am-path" placeholder="Model path (.gguf)">
<input type="number" id="am-gpu" placeholder="GPU layers" value="99" style="width:90px">
<input type="number" id="am-ctx" placeholder="Context" value="4096" style="width:80px">
<button class="btn btn-p btn-sm" onclick="addModel()">Add Model</button></div></div>
</div>
<!-- System -->
<div class="tc" id="t-system">
<div class="ig" id="sys-info"></div>
<div class="ig" style="margin-top:12px" id="cfg-info"></div></div>
<!-- Chat -->
<div class="tc" id="t-chat">
<div class="chat-wrap">
<div class="chat-header">
<label>Model:</label>
<select id="chat-model"></select>
<label>Max tokens:</label>
<input type="number" id="chat-max-tokens" value="1024" style="width:80px">
<label>Temp:</label>
<input type="number" id="chat-temp" value="0.7" step="0.1" min="0" max="2" style="width:65px">
<label><input type="checkbox" id="chat-stream" checked> Stream</label>
<span class="chat-clear" onclick="clearChat()">Clear chat</span>
</div>
<div class="chat-messages" id="chat-messages">
<div class="chat-empty" id="chat-empty">
<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M12 20.5C16.6944 20.5 20.5 16.6944 20.5 12C20.5 7.30558 16.6944 3.5 12 3.5C7.30558 3.5 3.5 7.30558 3.5 12C3.5 13.6708 3.96639 15.2302 4.77588 16.5576L3.5 20.5L7.44238 19.2241C8.76984 20.0336 10.3292 20.5 12 20.5Z"/></svg>
<p>Send a message to start chatting</p>
<p style="font-size:11px">Supports images — paste, drag &amp; drop, or click attach</p>
</div>
</div>
<div class="chat-drop-zone" id="chat-drop">Drop image here</div>
<div class="chat-input-area">
<div class="chat-attachments" id="chat-attachments"></div>
<div class="chat-input-row">
<textarea id="chat-input" placeholder="Type a message... (Shift+Enter for newline)" rows="1"></textarea>
<div class="actions">
<input type="file" id="chat-file" accept="image/*" multiple hidden>
<button class="chat-btn-attach" onclick="document.getElementById('chat-file').click()" title="Attach image"><svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21.44 11.05l-9.19 9.19a6 6 0 01-8.49-8.49l9.19-9.19a4 4 0 015.66 5.66l-9.2 9.19a2 2 0 01-2.83-2.83l8.49-8.48"/></svg></button>
<button class="chat-btn-send" id="chat-send" onclick="sendChat()" title="Send"><svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M22 2L11 13"/><path d="M22 2L15 22L11 13L2 9L22 2Z"/></svg></button>
</div>
</div>
</div>
</div>
</div>
<!-- Modal -->
<div class="modal-o" id="modal"><div class="modal"><h2><span>Request Detail</span><span class="close" onclick="closeModal()">&times;</span></h2><div id="modal-body"></div></div></div>

<script>
let D={},reqs=[],ts=[],keys={},sys={},cfg={},sla={},events=[],audit=[],health=[],hourly=[],compare=[],modelEvents=[],vram=[],disk=[],toggles={},schedules=[];
const $=id=>document.getElementById(id);
const J=v=>JSON.stringify(v);
const fmt=n=>(n==null)?'\u2014':typeof n==='number'?n.toLocaleString():n;
const fmtMs=n=>(n==null)?'\u2014':n<1000?Math.round(n)+'ms':(n/1000).toFixed(1)+'s';
const fmtT=t=>{try{return new Date(t).toLocaleTimeString()}catch(e){return t||'\u2014'}};
const fmtUp=s=>{const h=Math.floor(s/3600),m=Math.floor((s%3600)/60),ss=Math.floor(s%60);return h>0?h+'h '+m+'m':m>0?m+'m '+ss+'s':ss+'s'};
const lc=ms=>ms<500?'lf':ms<2000?'lm':ms<5000?'ls':'lb2';
const gc=p=>p<50?'var(--green)':p<80?'var(--yellow)':'var(--red)';
const esc=s=>{const d=document.createElement('div');d.textContent=s;return d.innerHTML};

document.querySelectorAll('.tab').forEach(t=>{t.onclick=()=>{
document.querySelectorAll('.tab').forEach(x=>x.classList.remove('active'));
document.querySelectorAll('.tc').forEach(x=>x.classList.remove('active'));
t.classList.add('active');$('t-'+t.dataset.t).classList.add('active')}});

// SSE
let sseConn;
function startSSE(){
try{sseConn=new EventSource('/dashboard/api/sse');
sseConn.onmessage=e=>{try{const r=JSON.parse(e.data);reqs.unshift(r);if(reqs.length>200)reqs.pop();renderRecentReqs()}catch(ex){}};
sseConn.onopen=()=>{$('sse-status').textContent='live'};
sseConn.onerror=()=>{$('sse-status').textContent='reconnecting...'}}catch(e){}}
startSSE();

async function fetchAll(){
try{const [dR,rR,tR,kR,sR,cR,slR,eR,aR,hR,hrR,cpR,meR,vrR,dkR,tgR,scR]=await Promise.all([
fetch('/dashboard/api/data'),fetch('/dashboard/api/requests?limit=200'),
fetch('/dashboard/api/timeseries'),fetch('/dashboard/api/keys'),
fetch('/dashboard/api/system'),fetch('/dashboard/api/config'),
fetch('/dashboard/api/sla'),fetch('/dashboard/api/events?limit=100'),
fetch('/dashboard/api/audit?limit=100'),fetch('/dashboard/api/health-history?limit=100'),
fetch('/dashboard/api/hourly'),fetch('/dashboard/api/compare'),
fetch('/dashboard/api/model-events?limit=100'),fetch('/dashboard/api/vram/'),
fetch('/dashboard/api/disk'),fetch('/dashboard/api/toggles'),fetch('/dashboard/api/schedule')
]);
D=await dR.json();reqs=await rR.json();ts=await tR.json();keys=await kR.json();
sys=await sR.json();cfg=await cR.json();sla=await slR.json();events=await eR.json();
audit=await aR.json();health=await hR.json();hourly=await hrR.json();compare=await cpR.json();
modelEvents=await meR.json();vram=await vrR.json();disk=await dkR.json();toggles=await tgR.json();
schedules=await scR.json();
render();
}catch(e){console.error('fetch',e)}}

function render(){
const m=D.metrics||{},ms=m.model_stats||{},models=D.models||[],bk=D.backends||[],gpu=D.gpu||[];
$('uptime').textContent=fmtUp(m.uptime_seconds||sys.uptime_sec||0);

// Alerts
let ah='';
bk.forEach(b=>{if(b.state==='failed')ah+='<div class="alert error">Backend <b>'+b.model_name+'</b> (port '+b.port+') FAILED</div>'});
if((D.queue_depth||0)>20)ah+='<div class="alert warn">Queue depth high: '+D.queue_depth+'</div>';
gpu.forEach(g=>{if(g.MemTotalMB>0&&(g.MemUsedMB/g.MemTotalMB)>.9)ah+='<div class="alert warn">GPU '+g.Index+' memory >90%</div>'});
if(sla&&!sla.compliant)ah+='<div class="alert warn">SLA non-compliant: P95='+fmtMs(sla.current_p95_ms)+' (target '+fmtMs(sla.target_p95_ms)+')</div>';
$('alerts').innerHTML=ah;

// Stats
const ct=(m.cache_hits||0)+(m.cache_misses||0),cr=ct>0?((m.cache_hits/ct)*100).toFixed(0)+'%':'\u2014';
const er=(m.requests_total||0)>0?((m.errors_total/m.requests_total)*100).toFixed(1)+'%':'0%';
$('stats').innerHTML='<div class="sc"><div class="lb">Requests</div><div class="vl">'+fmt(m.requests_total||0)+'</div><div class="sb">'+er+' errors</div></div>'+
'<div class="sc"><div class="lb">Active</div><div class="vl">'+fmt(m.active_requests||0)+'</div><div class="sb">in-flight</div></div>'+
'<div class="sc"><div class="lb">Models</div><div class="vl">'+fmt(m.loaded_models||0)+'/'+models.length+'</div><div class="sb">loaded/total</div></div>'+
'<div class="sc"><div class="lb">Queue</div><div class="vl">'+fmt(D.queue_depth||0)+'</div><div class="sb">waiting</div></div>'+
'<div class="sc"><div class="lb">Tokens</div><div class="vl">'+fmt(m.tokens_generated||0)+'</div><div class="sb">generated</div></div>'+
'<div class="sc"><div class="lb">Cache</div><div class="vl">'+cr+'</div><div class="sb">'+fmt(m.cache_hits||0)+' / '+fmt(ct)+'</div></div>';

// Backends
let bh='';
bk.forEach(b=>{bh+='<tr><td><b>'+b.model_name+'</b></td><td>'+b.port+'</td><td><span class="badge '+b.state+'">'+b.state+'</span></td><td>'+b.active_requests+'</td><td>'+fmtT(b.last_used)+'</td><td><button class="btn btn-d btn-sm" onclick="unloadModel(\''+b.model_name+'\')">Unload</button> <button class="btn btn-s btn-sm" onclick="warmupModel(\''+b.model_name+'\')">Warmup</button></td></tr>'});
if(!bh)bh='<tr><td colspan="6" style="text-align:center;color:var(--text-muted);padding:14px">No backends</td></tr>';
$('backends').innerHTML=bh;

renderRecentReqs();

// Models
let mc='';
models.forEach(mod=>{const s=ms[mod.name]||{},al=mod.aliases&&mod.aliases.length?mod.aliases.join(', '):'none',ld=mod.loaded;
mc+='<div class="mc"><h3><span class="badge '+(ld?'ready':'stopped')+'">'+(ld?'LOADED':'OFF')+'</span> '+mod.name+'</h3>';
mc+='<div class="al">Aliases: '+al+'</div>';
mc+='<div class="ms"><span class="k">Path</span><span class="v" style="font-size:10px;max-width:160px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">'+(mod.path||'auto')+'</span></div>';
mc+='<div class="ms"><span class="k">Requests</span><span class="v">'+fmt(s.requests||0)+'</span></div>';
mc+='<div class="ms"><span class="k">Avg</span><span class="v '+lc(s.avg_ms)+'">'+fmtMs(s.avg_ms)+'</span></div>';
mc+='<div class="ms"><span class="k">P50 / P95</span><span class="v">'+fmtMs(s.p50_ms)+' / '+fmtMs(s.p95_ms)+'</span></div>';
mc+='<div class="acts">';
mc+=ld?'<button class="btn btn-d btn-sm" onclick="unloadModel(\''+mod.name+'\')">Unload</button> <button class="btn btn-s btn-sm" onclick="warmupModel(\''+mod.name+'\')">Warmup</button>':'<button class="btn btn-p btn-sm" onclick="loadModel(\''+mod.name+'\')">Load</button>';
mc+='</div></div>'});
if(!mc)mc='<div class="card" style="text-align:center;color:var(--text-muted)">No models</div>';
$('model-cards').innerHTML=mc;

// Requests tab
renderReqsTab();

// Charts
drawChart('c-rps',ts.map(p=>p.rps),'#3b82f6');
drawChart('c-tps',ts.map(p=>p.tps),'#22c55e');
drawChart('c-lat',ts.map(p=>p.lat),'#f97316');
drawChart('c-gpu',ts.map(p=>p.gpu_pct),'#a855f7',100);
drawBarChart('c-hourly',hourly);

// Compare
let ct2='';
compare.forEach(c=>{ct2+='<tr><td><b>'+c.name+'</b></td><td>'+fmt(c.requests)+'</td><td class="'+lc(c.avg_ms)+'">'+fmtMs(c.avg_ms)+'</td><td>'+fmtMs(c.p50_ms)+'</td><td>'+fmtMs(c.p95_ms)+'</td><td>'+fmtMs(c.p99_ms)+'</td></tr>'});
if(!ct2)ct2='<tr><td colspan="6" style="text-align:center;color:var(--text-muted);padding:14px">No data yet</td></tr>';
$('cmp-table').innerHTML=ct2;

// GPU
let gh='';
gpu.forEach(g=>{const p=g.MemTotalMB>0?((g.MemUsedMB/g.MemTotalMB)*100):0;
gh+='<div class="card"><h3>GPU '+g.Index+': '+g.Name+'</h3><div style="display:flex;justify-content:space-between;margin-top:6px"><span style="font-size:20px;font-weight:700">'+(g.MemUsedMB||0)+' MB</span><span style="color:var(--text-muted)">/ '+g.MemTotalMB+' MB</span></div><div class="gpu-bar"><div class="gpu-fill" style="width:'+p.toFixed(1)+'%;background:'+gc(p)+'"></div></div><div style="display:flex;justify-content:space-between;margin-top:4px;font-size:11px;color:var(--text-muted)"><span>'+p.toFixed(1)+'%</span><span>'+g.MemFreeMB+' MB free</span></div></div>'});
if(!gh)gh='<div class="card" style="color:var(--text-muted);text-align:center">No GPU info</div>';
$('gpu-cards').innerHTML=gh;

// VRAM
let vt='';
(vram||[]).forEach(v=>{vt+='<tr><td>'+v.model+'</td><td>'+v.file_size_mb+' MB</td><td>'+v.est_vram_mb+' MB</td><td>'+v.gpu_layers+'</td><td>'+v.context_size+'</td><td>'+(v.can_fit?'<span class="badge ready">Yes</span>':'<span class="badge failed">No</span>')+'</td></tr>'});
if(!vt)vt='<tr><td colspan="6" style="text-align:center;color:var(--text-muted)">No data</td></tr>';
$('vram-table').innerHTML=vt;

// Disk
let dt='';
(disk||[]).forEach(d=>{dt+='<tr><td>'+d.model+'</td><td style="font-size:10px">'+d.path+'</td><td>'+d.size_mb+'</td><td>'+(d.exists?'<span class="badge ready">Yes</span>':'<span class="badge failed">No</span>')+'</td></tr>'});
$('disk-table').innerHTML=dt||'<tr><td colspan="4" style="text-align:center;color:var(--text-muted)">No data</td></tr>';

// Keys
let kh='';
Object.entries(keys).sort((a,b)=>b[1].requests-a[1].requests).forEach(([k,v])=>{kh+='<tr><td><code>'+k+'</code></td><td>'+fmt(v.requests)+'</td><td>'+fmt(v.tokens)+'</td><td>'+fmt(v.errors)+'</td><td>'+fmtT(v.last_request)+'</td></tr>'});
$('key-table').innerHTML=kh||'<tr><td colspan="5" style="text-align:center;color:var(--text-muted);padding:14px">No key usage</td></tr>';

// Events
let et='';
events.forEach(e=>{et+='<tr><td>'+fmtT(e.timestamp)+'</td><td><span class="badge '+e.level+'">'+e.level+'</span></td><td>'+e.source+'</td><td>'+(e.model||'\u2014')+'</td><td>'+esc(e.message)+'</td></tr>'});
$('event-table').innerHTML=et||'<tr><td colspan="5" style="text-align:center;color:var(--text-muted);padding:14px">No events</td></tr>';

let mt='';
modelEvents.forEach(e=>{mt+='<tr><td>'+fmtT(e.timestamp)+'</td><td><b>'+e.model+'</b></td><td><span class="badge '+(e.event.includes('crash')||e.event.includes('fail')?'failed':e.event==='loaded'?'ready':'info')+'">'+e.event+'</span></td><td>'+esc(e.detail||'')+'</td></tr>'});
$('timeline-table').innerHTML=mt||'<tr><td colspan="4" style="text-align:center;color:var(--text-muted);padding:14px">No events</td></tr>';

// Audit
let at='';
audit.forEach(a=>{at+='<tr><td>'+fmtT(a.timestamp)+'</td><td><span class="badge info">'+a.action+'</span></td><td>'+a.actor+'</td><td>'+a.target+'</td><td>'+esc(a.detail||'')+'</td></tr>'});
$('audit-table').innerHTML=at||'<tr><td colspan="5" style="text-align:center;color:var(--text-muted);padding:14px">No audit entries</td></tr>';

// Health
let ht='';
health.forEach(h=>{ht+='<tr><td>'+fmtT(h.timestamp)+'</td><td>'+h.model+'</td><td>'+h.port+'</td><td>'+(h.ok?'<span class="badge ready">OK</span>':'<span class="badge failed">FAIL</span>')+'</td><td>'+fmtMs(h.latency_ms)+'</td></tr>'});
$('health-table').innerHTML=ht||'<tr><td colspan="5" style="text-align:center;color:var(--text-muted);padding:14px">No checks</td></tr>';

// SLA
$('sla-cards').innerHTML='<div class="ii"><div class="il">P95 Latency</div><div class="iv '+(sla.compliant?'lf':'lb2')+'">'+fmtMs(sla.current_p95_ms)+' / '+fmtMs(sla.target_p95_ms)+'</div><div class="sla-meter"><div class="sla-fill" style="width:'+Math.min(100,(sla.target_p95_ms>0?(sla.current_p95_ms/sla.target_p95_ms*100):0)).toFixed(0)+'%;background:'+(sla.compliant?'var(--green)':'var(--red)')+'"></div></div></div>'+
'<div class="ii"><div class="il">Uptime</div><div class="iv">'+sla.uptime_pct.toFixed(2)+'%</div></div>'+
'<div class="ii"><div class="il">Error Budget</div><div class="iv">'+(sla.error_budget_pct||0).toFixed(2)+'% remaining</div></div>'+
'<div class="ii"><div class="il">Health Checks</div><div class="iv">'+fmt(sla.passed_checks)+' / '+fmt(sla.total_checks)+' passed</div></div>'+
'<div class="ii"><div class="il">Status</div><div class="iv">'+(sla.compliant?'<span class="badge ready">COMPLIANT</span>':'<span class="badge failed">NON-COMPLIANT</span>')+'</div></div>';

// Schedules
let schOpts='';
(D.models||[]).forEach(m=>{schOpts+='<option value="'+m.name+'">'+m.name+'</option>'});
$('sched-model').innerHTML=schOpts;
let st='';
(schedules||[]).forEach(s=>{st+='<tr><td>'+s.id+'</td><td>'+s.type+'</td><td>'+s.model+'</td><td>'+s.after_min+' min</td><td><button class="btn btn-d btn-sm" onclick="removeSched(\''+s.id+'\')">Remove</button></td></tr>'});
$('sched-table').innerHTML=st||'<tr><td colspan="5" style="text-align:center;color:var(--text-muted);padding:14px">No rules</td></tr>';

// Toggles
let tg='';
Object.entries(toggles).forEach(([k,v])=>{tg+='<div class="ii" style="cursor:pointer" onclick="toggleFeature(\''+k+'\','+(!v)+')"><div class="il">'+k.replace('_',' ')+'</div><div class="iv">'+(v?'<span style="color:var(--green)">ON</span>':'<span style="color:var(--red)">OFF</span>')+'</div></div>'});
$('toggles').innerHTML=tg;

// Config editor - only load once
if(!$('cfg-editor').value){
fetch('/dashboard/api/config/edit').then(r=>r.json()).then(d=>{$('cfg-editor').value=d.content||''}).catch(()=>{});}

// System
$('sys-info').innerHTML='<div class="ii"><div class="il">Hostname</div><div class="iv">'+(sys.hostname||'\u2014')+'</div></div>'+
'<div class="ii"><div class="il">Go</div><div class="iv">'+(sys.go_version||'')+'</div></div>'+
'<div class="ii"><div class="il">OS/Arch</div><div class="iv">'+(sys.os||'')+'/'+( sys.arch||'')+'</div></div>'+
'<div class="ii"><div class="il">CPUs</div><div class="iv">'+(sys.num_cpu||'')+'</div></div>'+
'<div class="ii"><div class="il">Goroutines</div><div class="iv">'+fmt(sys.num_goroutine)+'</div></div>'+
'<div class="ii"><div class="il">Memory</div><div class="iv">'+fmt(sys.mem_alloc_mb)+' / '+fmt(sys.mem_sys_mb)+' MB</div></div>'+
'<div class="ii"><div class="il">Uptime</div><div class="iv">'+fmtUp(sys.uptime_sec||0)+'</div></div>';

$('cfg-info').innerHTML='<div class="ii"><div class="il">Listen</div><div class="iv">'+(cfg.listen_addr||'')+'</div></div>'+
'<div class="ii"><div class="il">Max Models</div><div class="iv">'+(cfg.max_loaded_models||'')+'</div></div>'+
'<div class="ii"><div class="il">Health Interval</div><div class="iv">'+(cfg.health_check_sec||'')+'s</div></div>'+
'<div class="ii"><div class="il">Logging</div><div class="iv">'+(cfg.logging_format||'text')+'</div></div>';
}

function renderRecentReqs(){
const list=(reqs||[]).slice(0,15);
let h='';
list.forEach(r=>{const bd=[],tk=(r.prompt_tokens||0)+(r.completion_tokens||0);
if(r.is_error)bd.push('<span class="badge error">'+r.status+'</span>');else bd.push('<span class="badge ready">'+r.status+'</span>');
if(r.is_stream)bd.push('<span class="badge stream">SSE</span>');if(r.is_cache_hit)bd.push('<span class="badge cached">HIT</span>');
h+='<tr class="clickable" onclick="showReq(\''+r.id+'\')"><td>'+fmtT(r.timestamp)+'</td><td>'+r.model+'</td><td>'+r.endpoint+'</td><td class="'+lc(r.duration_ms)+'">'+fmtMs(r.duration_ms)+'</td><td>'+tk+'</td><td>'+bd.join(' ')+'</td></tr>'});
if(!h)h='<tr><td colspan="6" style="text-align:center;color:var(--text-muted);padding:14px">No requests</td></tr>';
$('recent-reqs').innerHTML=h;
}

function renderReqsTab(){
const f=$('rf').value;
let fl=reqs||[];
if(f==='errors')fl=fl.filter(r=>r.is_error);
else if(f==='slow')fl=fl.filter(r=>r.duration_ms>2000);
else if(f==='cached')fl=fl.filter(r=>r.is_cache_hit);
let h='';
fl.forEach(r=>{const bd=[],tk=(r.prompt_tokens||0)+(r.completion_tokens||0);
if(r.is_error)bd.push('<span class="badge error">'+r.status+'</span>');else bd.push('<span class="badge ready">'+r.status+'</span>');
if(r.is_stream)bd.push('<span class="badge stream">SSE</span>');if(r.is_cache_hit)bd.push('<span class="badge cached">HIT</span>');
h+='<tr class="clickable" onclick="showReq(\''+r.id+'\')"><td>'+fmtT(r.timestamp)+'</td><td style="font-size:10px;max-width:100px;overflow:hidden;text-overflow:ellipsis">'+r.id+'</td><td>'+r.model+'</td><td>'+r.endpoint+'</td><td class="'+lc(r.duration_ms)+'">'+fmtMs(r.duration_ms)+'</td><td>'+tk+'</td><td>'+bd.join(' ')+'</td><td style="font-size:10px">'+r.remote_addr+'</td></tr>'});
if(!h)h='<tr><td colspan="8" style="text-align:center;color:var(--text-muted);padding:14px">No requests</td></tr>';
$('all-reqs').innerHTML=h;
}
$('rf').onchange=renderReqsTab;

function showReq(id){
const r=(reqs||[]).find(x=>x.id===id);if(!r)return;
const flds=[['ID',r.id],['Time',r.timestamp],['Model',r.model],['Endpoint',r.endpoint],
['Duration',fmtMs(r.duration_ms)],['Status',r.status+(r.is_error?' (error)':'')],
['Stream',r.is_stream?'Yes':'No'],['Cache Hit',r.is_cache_hit?'Yes':'No'],
['Prompt Tokens',r.prompt_tokens],['Completion Tokens',r.completion_tokens],
['Queue',fmtMs(r.queue_ms)],['Load',fmtMs(r.load_ms)],['Inference',fmtMs(r.inference_ms)],
['Remote',r.remote_addr],['API Key',r.api_key||'none']];
let h=flds.map(([l,v])=>'<div class="fld"><div class="fl">'+l+'</div><div class="fv">'+v+'</div></div>').join('');
if(r.prompt)h+='<div class="fld"><div class="fl">Prompt</div><pre>'+esc(r.prompt)+'</pre></div>';
if(r.response)h+='<div class="fld"><div class="fl">Response</div><pre>'+esc(r.response)+'</pre></div>';
$('modal-body').innerHTML=h;$('modal').classList.add('active');
}
function closeModal(){$('modal').classList.remove('active')}
$('modal').onclick=e=>{if(e.target===$('modal'))closeModal()};

function drawChart(id,vals,color,fixMax){
const cv=$(id);if(!cv)return;const ctx=cv.getContext('2d');
const r=cv.parentElement.getBoundingClientRect(),d=window.devicePixelRatio||1;
cv.width=r.width*d;cv.height=r.height*d;ctx.scale(d,d);
const w=r.width,h=r.height;ctx.clearRect(0,0,w,h);
if(!vals||vals.length<2){ctx.fillStyle='#475569';ctx.font='12px sans-serif';ctx.textAlign='center';ctx.fillText('Waiting...',w/2,h/2);return}
const mx=fixMax||Math.max(...vals,1)*1.1,pd={t:8,b:20,l:44,r:8},cw=w-pd.l-pd.r,ch=h-pd.t-pd.b,st=cw/(vals.length-1);
ctx.strokeStyle='#1e293b';ctx.lineWidth=1;
for(let i=0;i<=4;i++){const y=pd.t+(ch/4)*i;ctx.beginPath();ctx.moveTo(pd.l,y);ctx.lineTo(w-pd.r,y);ctx.stroke();
ctx.fillStyle='#475569';ctx.font='9px sans-serif';ctx.textAlign='right';ctx.fillText(((4-i)/4*mx).toFixed(mx>100?0:1),pd.l-4,y+3)}
ctx.beginPath();ctx.moveTo(pd.l,pd.t+ch);
vals.forEach((v,i)=>{ctx.lineTo(pd.l+i*st,pd.t+ch-(v/mx)*ch)});
ctx.lineTo(pd.l+(vals.length-1)*st,pd.t+ch);ctx.closePath();ctx.fillStyle=color+'18';ctx.fill();
ctx.beginPath();vals.forEach((v,i)=>{const x=pd.l+i*st,y=pd.t+ch-(v/mx)*ch;i===0?ctx.moveTo(x,y):ctx.lineTo(x,y)});
ctx.strokeStyle=color;ctx.lineWidth=1.5;ctx.stroke();
const last=vals[vals.length-1];ctx.fillStyle=color;ctx.font='bold 11px sans-serif';ctx.textAlign='right';ctx.fillText(last.toFixed(last>100?0:2),w-pd.r,pd.t-1);
}

function drawBarChart(id,data){
const cv=$(id);if(!cv||!data||!data.length)return;const ctx=cv.getContext('2d');
const r=cv.parentElement.getBoundingClientRect(),d=window.devicePixelRatio||1;
cv.width=r.width*d;cv.height=r.height*d;ctx.scale(d,d);
const w=r.width,h=r.height,pd={t:8,b:30,l:44,r:8};ctx.clearRect(0,0,w,h);
const cw=w-pd.l-pd.r,ch=h-pd.t-pd.b,mx=Math.max(...data.map(b=>b.requests),1)*1.1;
const bw=Math.max(2,cw/data.length-2);
ctx.strokeStyle='#1e293b';ctx.lineWidth=1;
for(let i=0;i<=4;i++){const y=pd.t+(ch/4)*i;ctx.beginPath();ctx.moveTo(pd.l,y);ctx.lineTo(w-pd.r,y);ctx.stroke();
ctx.fillStyle='#475569';ctx.font='9px sans-serif';ctx.textAlign='right';ctx.fillText(Math.round((4-i)/4*mx),pd.l-4,y+3)}
data.forEach((b,i)=>{const x=pd.l+i*(cw/data.length),bh=(b.requests/mx)*ch;
ctx.fillStyle='#3b82f680';ctx.fillRect(x,pd.t+ch-bh,bw,bh);
if(i%Math.ceil(data.length/8)===0){ctx.fillStyle='#475569';ctx.font='8px sans-serif';ctx.textAlign='center';
ctx.fillText(b.hour.slice(11)||b.hour.slice(5,10),x+bw/2,h-pd.b+12)}});
}

async function loadModel(n){try{const b=event.target;b.disabled=true;b.textContent='Loading...';await fetch('/dashboard/api/load',{method:'POST',headers:{'Content-Type':'application/json'},body:J({model:n})});await fetchAll()}catch(e){alert(e)}}
async function unloadModel(n){try{await fetch('/dashboard/api/unload',{method:'POST',headers:{'Content-Type':'application/json'},body:J({model:n})});await fetchAll()}catch(e){alert(e)}}
async function warmupModel(n){try{const r=await fetch('/dashboard/api/warmup',{method:'POST',headers:{'Content-Type':'application/json'},body:J({model:n})});const d=await r.json();alert(d.status||d.error)}catch(e){alert(e)}}
async function saveConfig(){try{const r=await fetch('/dashboard/api/config/edit',{method:'POST',headers:{'Content-Type':'application/json'},body:J({content:$('cfg-editor').value})});const d=await r.json();alert(d.status||d.error)}catch(e){alert(e)}}
async function reloadConfig(){try{await fetch('/dashboard/api/config/reload',{method:'POST'});alert('Reloaded');await fetchAll()}catch(e){alert(e)}}
async function toggleFeature(f,v){try{await fetch('/dashboard/api/toggles',{method:'POST',headers:{'Content-Type':'application/json'},body:J({feature:f,enabled:v})});await fetchAll()}catch(e){alert(e)}}
async function addSchedule(){const m=$('sched-model').value,mn=parseInt($('sched-min').value);if(!m||!mn)return;try{await fetch('/dashboard/api/schedule',{method:'POST',headers:{'Content-Type':'application/json'},body:J({type:'unload_idle',model:m,after_min:mn})});await fetchAll()}catch(e){alert(e)}}
async function removeSched(id){try{await fetch('/dashboard/api/schedule',{method:'DELETE',headers:{'Content-Type':'application/json'},body:J({id:id})});await fetchAll()}catch(e){alert(e)}}
async function createKey(){try{const r=await fetch('/dashboard/api/keys/manage',{method:'POST',headers:{'Content-Type':'application/json'},body:J({action:'create'})});const d=await r.json();if(d.key)prompt('New API Key (copy now):',d.key);await fetchAll()}catch(e){alert(e)}}
async function addModel(){const n=$('am-name').value,p=$('am-path').value,g=parseInt($('am-gpu').value)||0,c=parseInt($('am-ctx').value)||4096;
if(!n||!p){alert('Name and path required');return}
try{await fetch('/dashboard/api/model/add',{method:'POST',headers:{'Content-Type':'application/json'},body:J({name:n,model_path:p,gpu_layers:g,context_size:c})});alert('Model added');await fetchAll()}catch(e){alert(e)}}

fetchAll();setInterval(fetchAll,3000);

// ============ CHAT ============
let chatHistory=[];
let chatImages=[];
let chatAbort=null;
let chatStreaming=false;

function populateChatModels(){
const sel=$('chat-model');
if(!sel||!D.models)return;
const cur=sel.value;
sel.innerHTML='';
(D.models||[]).forEach(m=>{
const o=document.createElement('option');o.value=m.name;o.textContent=m.name;
if(m.aliases&&m.aliases.length)o.textContent+=' ('+m.aliases[0]+')';
sel.appendChild(o);
});
if(cur)sel.value=cur;
}

const origRender=render;
render=function(){origRender();populateChatModels();};

// Auto-resize textarea
const chatInput=$('chat-input');
chatInput.addEventListener('input',function(){
this.style.height='auto';
this.style.height=Math.min(this.scrollHeight,200)+'px';
});
chatInput.addEventListener('keydown',function(e){
if(e.key==='Enter'&&!e.shiftKey){e.preventDefault();sendChat();}
});

// Image handling
$('chat-file').addEventListener('change',function(e){
Array.from(e.target.files).forEach(f=>addChatImage(f));
e.target.value='';
});

// Paste handler
chatInput.addEventListener('paste',function(e){
const items=e.clipboardData&&e.clipboardData.items;
if(!items)return;
for(let i=0;i<items.length;i++){
if(items[i].type.indexOf('image')!==-1){
e.preventDefault();
addChatImage(items[i].getAsFile());
}
}
});

// Drag & drop
const chatWrap=document.querySelector('.chat-wrap');
let dragCounter=0;
if(chatWrap){
chatWrap.addEventListener('dragenter',function(e){e.preventDefault();dragCounter++;$('chat-drop').classList.add('active');});
chatWrap.addEventListener('dragleave',function(e){e.preventDefault();dragCounter--;if(dragCounter<=0){dragCounter=0;$('chat-drop').classList.remove('active');}});
chatWrap.addEventListener('dragover',function(e){e.preventDefault();});
chatWrap.addEventListener('drop',function(e){e.preventDefault();dragCounter=0;$('chat-drop').classList.remove('active');
Array.from(e.dataTransfer.files).filter(f=>f.type.startsWith('image/')).forEach(f=>addChatImage(f));
});
}

function addChatImage(file){
const reader=new FileReader();
reader.onload=function(e){
const dataUrl=e.target.result;
chatImages.push({dataUrl:dataUrl,type:file.type,name:file.name});
renderChatAttachments();
};
reader.readAsDataURL(file);
}

function removeChatImage(idx){
chatImages.splice(idx,1);
renderChatAttachments();
}

function renderChatAttachments(){
const c=$('chat-attachments');
c.innerHTML=chatImages.map((img,i)=>
'<div class="chat-attachment"><img src="'+img.dataUrl+'" alt="'+esc(img.name)+'"><div class="remove" onclick="removeChatImage('+i+')">&times;</div></div>'
).join('');
}

function clearChat(){
chatHistory=[];
chatImages=[];
renderChatMessages();
renderChatAttachments();
}

function renderChatMessages(){
const box=$('chat-messages');
if(!chatHistory.length){
box.innerHTML='<div class="chat-empty"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M12 20.5C16.6944 20.5 20.5 16.6944 20.5 12C20.5 7.30558 16.6944 3.5 12 3.5C7.30558 3.5 3.5 7.30558 3.5 12C3.5 13.6708 3.96639 15.2302 4.77588 16.5576L3.5 20.5L7.44238 19.2241C8.76984 20.0336 10.3292 20.5 12 20.5Z"/></svg><p>Send a message to start chatting</p><p style="font-size:11px">Supports images \u2014 paste, drag &amp; drop, or click attach</p></div>';
return;
}
let html='';
chatHistory.forEach((msg,idx)=>{
const isUser=msg.role==='user';
html+='<div class="chat-msg '+(isUser?'user':'assistant')+'">';
html+='<div class="avatar">'+(isUser?'U':'AI')+'</div>';
html+='<div style="display:flex;flex-direction:column">';
html+='<div class="bubble">';
if(msg.images&&msg.images.length){
msg.images.forEach(img=>{html+='<img src="'+img+'" alt="attached">';});
}
html+=formatChatContent(msg.content||'');
if(msg.loading)html+='<span class="typing-dot"></span><span class="typing-dot"></span><span class="typing-dot"></span>';
html+='</div>';
if(msg.tokens)html+='<div class="meta">'+msg.tokens+' tokens'+(msg.duration?' &middot; '+msg.duration:'')+'</div>';
html+='</div></div>';
});
box.innerHTML=html;
box.scrollTop=box.scrollHeight;
}

function formatChatContent(text){
if(!text)return '';
// Handle think tags
text=text.replace(/<think>([\s\S]*?)(<\/think>|$)/gi,function(m,inner){
return '<div class="thinking">'+esc(inner.trim())+'</div>';
});
// Simple code block rendering
var codeBlockRe=new RegExp(String.fromCharCode(96,96,96)+'(\\w*)\\n?([\\s\\S]*?)'+String.fromCharCode(96,96,96),'g');
text=text.replace(codeBlockRe,function(m,lang,code){
return '<pre><code>'+esc(code.trim())+'</code></pre>';
});
// Inline code
var inlineCodeRe=new RegExp(String.fromCharCode(96)+'([^'+String.fromCharCode(96)+']+)'+String.fromCharCode(96),'g');
text=text.replace(inlineCodeRe,'<code>$1</code>');
// Bold
text=text.replace(/\*\*([^*]+)\*\*/g,'<b>$1</b>');
return text;
}

async function sendChat(){
const input=$('chat-input');
const text=input.value.trim();
if(!text&&!chatImages.length)return;
if(chatStreaming)return;

const model=$('chat-model').value;
if(!model){alert('Select a model first');return;}

const maxTokens=parseInt($('chat-max-tokens').value)||1024;
const temperature=parseFloat($('chat-temp').value)||0.7;
const doStream=$('chat-stream').checked;

// Build user message content
let userContent;
if(chatImages.length){
userContent=[];
chatImages.forEach(img=>{
userContent.push({type:'image_url',image_url:{url:img.dataUrl}});
});
if(text)userContent.push({type:'text',text:text});
}else{
userContent=text;
}

const userMsg={role:'user',content:text,images:chatImages.map(i=>i.dataUrl)};
chatHistory.push(userMsg);
input.value='';input.style.height='auto';
chatImages=[];
renderChatAttachments();

// Add assistant placeholder
const assistantMsg={role:'assistant',content:'',loading:true};
chatHistory.push(assistantMsg);
renderChatMessages();

// Build messages array for API
const messages=chatHistory.slice(0,-1).map(m=>{
if(m.role==='user'&&m.images&&m.images.length){
const parts=m.images.map(url=>({type:'image_url',image_url:{url:url}}));
if(m.content)parts.push({type:'text',text:m.content});
return {role:'user',content:parts};
}
return {role:m.role,content:m.content};
});
// Add current user message
messages.push({role:'user',content:userContent});

const body={model:model,messages:messages,max_tokens:maxTokens,temperature:temperature,stream:doStream};

const sendBtn=$('chat-send');
chatStreaming=true;
setSendBtn(true);

try{
if(doStream){
chatAbort=new AbortController();
const startT=Date.now();
const resp=await fetch('/v1/chat/completions',{
method:'POST',headers:{'Content-Type':'application/json'},
body:JSON.stringify(body),signal:chatAbort.signal
});
if(!resp.ok){
const err=await resp.text();
assistantMsg.content='Error: '+err;
assistantMsg.loading=false;
renderChatMessages();
return;
}
const reader=resp.body.getReader();
const decoder=new TextDecoder();
let fullText='',totalTokens=0;
assistantMsg.loading=false;

while(true){
const {done,value}=await reader.read();
if(done)break;
const chunk=decoder.decode(value,{stream:true});
const lines=chunk.split('\n');
for(const line of lines){
if(!line.startsWith('data: '))continue;
const data=line.slice(6).trim();
if(data==='[DONE]')break;
try{
const j=JSON.parse(data);
const delta=j.choices&&j.choices[0]&&j.choices[0].delta;
if(delta&&delta.content){
fullText+=delta.content;
assistantMsg.content=fullText;
totalTokens++;
}
}catch(ex){}
}
renderChatMessages();
}
const elapsed=((Date.now()-startT)/1000).toFixed(1)+'s';
assistantMsg.content=fullText;
assistantMsg.tokens=totalTokens;
assistantMsg.duration=elapsed;
renderChatMessages();

}else{
const startT=Date.now();
const resp=await fetch('/v1/chat/completions',{
method:'POST',headers:{'Content-Type':'application/json'},
body:JSON.stringify(body)
});
const data=await resp.json();
const elapsed=((Date.now()-startT)/1000).toFixed(1)+'s';
if(data.error){
assistantMsg.content='Error: '+(data.error.message||JSON.stringify(data.error));
}else{
const choice=data.choices&&data.choices[0];
assistantMsg.content=(choice&&choice.message&&choice.message.content)||'(empty response)';
assistantMsg.tokens=data.usage?data.usage.completion_tokens:null;
}
assistantMsg.loading=false;
assistantMsg.duration=elapsed;
renderChatMessages();
}
}catch(e){
if(e.name==='AbortError'){
assistantMsg.content+='\n\n(stopped)';
}else{
assistantMsg.content='Error: '+e.message;
}
assistantMsg.loading=false;
renderChatMessages();
}finally{
chatStreaming=false;
chatAbort=null;
setSendBtn(false);
}
}

function setSendBtn(streaming){
const btn=$('chat-send');
if(streaming){
btn.innerHTML='<svg width="18" height="18" viewBox="0 0 24 24" fill="currentColor"><rect x="6" y="6" width="12" height="12" rx="2"/></svg>';
btn.className='chat-btn-stop';
btn.onclick=()=>{if(chatAbort)chatAbort.abort();};
btn.title='Stop';
}else{
btn.innerHTML='<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M22 2L11 13"/><path d="M22 2L15 22L11 13L2 9L22 2Z"/></svg>';
btn.className='chat-btn-send';
btn.onclick=sendChat;
btn.title='Send';
}
}
</script>
</body>
</html>`
