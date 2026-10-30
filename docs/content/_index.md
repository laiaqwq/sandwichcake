---
title: sandwichcake
layout: hextra-home
---

<div class="sc-hero">
<div class="sc-hero-text">
{{< hextra/hero-badge link="docs/getting-started/installation" >}}
  No Dockerfile · No daemon
{{< /hextra/hero-badge >}}
<div class="hx:mt-6 hx:mb-6">
{{< hextra/hero-headline >}}
  Build container images by&nbsp;<br class="hx:sm:block hx:hidden" />stacking prebuilt layers
{{< /hextra/hero-headline >}}
</div>
<div class="hx:mb-12">
{{< hextra/hero-subtitle >}}
  Pack your venvs, model weights and binaries once.&nbsp;<br class="hx:sm:block hx:hidden" />Compose them onto any base image in seconds — only what changed is uploaded.
{{< /hextra/hero-subtitle >}}
</div>
<div class="sc-hero-actions">
{{< hextra/hero-button text="Get Started" link="docs/getting-started/quickstart" >}}
<a class="sc-link-button" href="#how-it-works">See how it works ↓</a>
</div>
</div>
<div class="sc-hero-logo"><img src="images/logo.svg" alt="" width="320" height="420" /></div>
</div>

<section class="sc-demo" id="how-it-works" data-sc-demo aria-labelledby="sc-demo-title">
<ol class="sc-sr-only">
<li>pack weight streams a model from Hugging Face through the local machine straight into registry blobs; nothing is written to local disk.</li>
<li>compose stacks the weight slice onto python:3.12-slim and pushes image:serve — the weight layer is mounted, not uploaded.</li>
<li>compose reuses the very same weight layer on a CUDA base image: mounted again, 0 B uploaded.</li>
</ol>
<div class="sc-demo-track">
<div class="sc-demo-stage">
<h2 class="sc-h2" id="sc-demo-title">How it works</h2>
<nav class="sc-progress" aria-label="Demo steps">
<button type="button" class="sc-step" data-step="1"><span class="sc-step-head"><span class="sc-step-num">1</span><span><b>Pack</b><span class="sc-step-desc"> — stream weights, never touch disk</span></span></span><span class="sc-seg" style="--f: var(--f1)"><i></i></span></button>
<button type="button" class="sc-step" data-step="2"><span class="sc-step-head"><span class="sc-step-num">2</span><span><b>Compose</b><span class="sc-step-desc"> — stack onto a base</span></span></span><span class="sc-seg" style="--f: var(--f2)"><i></i></span></button>
<button type="button" class="sc-step" data-step="3"><span class="sc-step-head"><span class="sc-step-num">3</span><span><b>Reuse</b><span class="sc-step-desc"> — same layer, another base</span></span></span><span class="sc-seg" style="--f: var(--f3)"><i></i></span></button>
</nav>
<div class="sc-demo-body" aria-hidden="true">
<div class="sc-term">
<div class="sc-term-bar"><i></i><i></i><i></i></div>
<pre class="sc-term-body"><span class="sc-l sc-cmd" data-at="0.02,0.12" data-type="$ sandwichcake pack weight hf://Qwen/Qwen3-8B@main">$ sandwichcake pack weight hf://Qwen/Qwen3-8B@main</span>
<span class="sc-l" data-at="0.13,0.14" data-type="  streaming → registry.example.com/ai/weights:Qwen--Qwen3-8B">  streaming → registry.example.com/ai/weights:Qwen--Qwen3-8B</span>
<span class="sc-l sc-ok" data-at="0.27,0.28" data-type="  15.3GiB uploaded · 0 B written to local disk">  15.3GiB uploaded · 0 B written to local disk</span>
<span class="sc-l sc-cmd" data-at="0.35,0.42" data-type="$ sandwichcake compose serve">$ sandwichcake compose serve</span>
<span class="sc-l" data-at="0.43,0.44" data-type="  + python:3.12-slim (base)           copy">  + python:3.12-slim (base)           copy</span>
<span class="sc-l" data-at="0.49,0.50" data-type="  ~ Qwen--Qwen3-8B (weight)  15.3GiB  mount">  ~ Qwen--Qwen3-8B (weight)  15.3GiB  mount</span>
<span class="sc-l sc-ok" data-at="0.59,0.60" data-type="  pushed registry.example.com/ai/infer:serve">  pushed registry.example.com/ai/infer:serve</span>
<span class="sc-l sc-cmd" data-at="0.68,0.75" data-type="$ sandwichcake compose serve-cuda">$ sandwichcake compose serve-cuda</span>
<span class="sc-l" data-at="0.76,0.77" data-type="  + cuda:12.4.1-runtime (base)        copy">  + cuda:12.4.1-runtime (base)        copy</span>
<span class="sc-l" data-at="0.82,0.83" data-type="  ~ Qwen--Qwen3-8B (weight)  15.3GiB  mount">  ~ Qwen--Qwen3-8B (weight)  15.3GiB  mount</span>
<span class="sc-l sc-ok" data-at="0.91,0.92" data-type="  pushed registry.example.com/ai/infer:serve-cuda">  pushed registry.example.com/ai/infer:serve-cuda</span></pre>
</div>
<div class="sc-viz">
<div class="sc-lane"></div>
<div class="sc-node sc-node-hf"><small>Hugging Face</small><b>Qwen/Qwen3-8B</b></div>
<div class="sc-node sc-node-local"><small>Local machine</small><b class="sc-zero">0 B</b><small>on disk</small></div>
<div class="sc-node sc-node-reg"><small>Registry</small>
<div class="sc-chip sc-chip-src"><span class="sc-fill"></span><span class="sc-chip-label">Qwen3-8B</span></div>
</div>
<div class="sc-packets"><i></i><i></i><i></i><i></i><i></i><i></i></div>
<div class="sc-stack sc-stack-a">
<div class="sc-stack-tag">image:serve</div>
<div class="sc-chip sc-chip-w sc-chip-wa"><span class="sc-chip-label">weights · Qwen3-8B</span></div>
<div class="sc-chip sc-chip-base sc-base-a"><span class="sc-chip-label">python:3.12-slim</span></div>
</div>
<div class="sc-stack sc-stack-b">
<div class="sc-stack-tag">image:serve-cuda</div>
<div class="sc-chip sc-chip-w sc-chip-wb"><span class="sc-chip-label">weights · Qwen3-8B</span></div>
<div class="sc-chip sc-chip-base sc-base-b"><span class="sc-chip-label">cuda:12.4.1-runtime</span></div>
<div class="sc-badge">~ mounted · 0 B uploaded</div>
</div>
</div>
</div>
</div>
<div class="sc-demo-spacer"></div>
</div>
</section>

<section class="sc-section" aria-labelledby="sc-why-title">
<h2 class="sc-h2" id="sc-why-title">Why not just a Dockerfile?</h2>
<div class="sc-vs-grid">
<article class="sc-vs">
<h3><svg class="sc-vs-icon" viewBox="0 0 24 24" aria-hidden="true"><ellipse cx="12" cy="6" rx="8" ry="3" /><path d="M4 6v12c0 1.7 3.6 3 8 3s8-1.3 8-3V6M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3" /></svg>Disk used to pack a 16 GB model</h3>
<div class="sc-vs-row sc-bad"><span class="sc-vs-who">Dockerfile</span><span class="sc-meter"><i style="width:100%"></i></span><b class="sc-vs-val">2–3× model size</b></div>
<div class="sc-vs-row sc-good"><span class="sc-vs-who">sandwichcake</span><span class="sc-meter"><i style="width:0"></i></span><b class="sc-vs-val">0 B · streamed</b></div>
</article>
<article class="sc-vs">
<h3><svg class="sc-vs-icon" viewBox="0 0 24 24" aria-hidden="true"><rect x="3" y="4" width="18" height="5" rx="1" /><path d="M3 14h7v6H3zM14 14h7v6h-7z" /><path d="M8 9v5M16 9v5" /></svg>Same weights on two base images</h3>
<div class="sc-vs-row sc-bad"><span class="sc-vs-who">Dockerfile</span><span class="sc-minis"><span class="sc-mini"><i class="w"></i><i class="b"></i></span><span class="sc-mini"><i class="w"></i><i class="b2"></i></span></span><b class="sc-vs-val">built per base</b></div>
<div class="sc-vs-row sc-good"><span class="sc-vs-who">sandwichcake</span><span class="sc-minis sc-minis-shared"><i class="w"></i><span class="sc-mini"><i class="b"></i></span><span class="sc-mini"><i class="b2"></i></span></span><b class="sc-vs-val">stored once · mounted</b></div>
</article>
<article class="sc-vs">
<h3><svg class="sc-vs-icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M21 12a9 9 0 1 1-3-6.7L21 8" /><path d="M21 3v5h-5" /></svg>Change one dependency</h3>
<div class="sc-vs-row sc-bad"><span class="sc-vs-who">Dockerfile</span><span class="sc-layers"><i></i><i class="hot"></i><i class="hot"></i><i class="hot"></i></span><b class="sc-vs-val">3 layers rebuilt</b></div>
<div class="sc-vs-row sc-good"><span class="sc-vs-who">sandwichcake</span><span class="sc-layers"><i></i><i class="hot"></i><i></i><i></i></span><b class="sc-vs-val">1 slice swapped</b></div>
</article>
<article class="sc-vs">
<h3><svg class="sc-vs-icon" viewBox="0 0 24 24" aria-hidden="true"><rect x="3" y="4" width="18" height="16" rx="2" /><path d="M7 9l3 3-3 3M13 15h4" /></svg>What it needs to run</h3>
<div class="sc-vs-row sc-bad"><span class="sc-vs-who">Dockerfile</span><span class="sc-tags"><em>daemon</em><em>BuildKit</em><em>privileged</em></span><b class="sc-vs-val">DinD in CI</b></div>
<div class="sc-vs-row sc-good"><span class="sc-vs-who">sandwichcake</span><span class="sc-tags"><em>1 Go binary</em></span><b class="sc-vs-val">runs unprivileged</b></div>
</article>
</div>
</section>

<section class="sc-cta">
{{< hextra/hero-button text="Read the Quickstart" link="docs/getting-started/quickstart" >}}
<a class="sc-link-button" href="https://github.com/laiaqwq/sandwichcake" target="_blank" rel="noreferrer">GitHub →</a>
</section>
