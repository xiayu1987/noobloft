English | [简体中文](COMPLIANCE.zh-CN.md)

# noobloft Liability Avoidance and Compliance Design

> This document is an engineering design constraint, not legal advice. Have a licensed attorney review it for each target market before any public release.
> Verified on 2026-09-25 against official primary sources.

## Current engineering boundary

This document contains historical design requirements. It is not a feature checklist or a compliance conclusion. This revision only syncs the engineering boundary; the legal sources were not re-verified.
Private mode uses a PSK by default. Explicit shared mode uses publisher delegations and per-consumer authorization; see [PUBLISHERS.md](PUBLISHERS.md).
Shared mode is not anonymous open inference: the model endpoint verifies credentials, and shared relays use a PeerID allowlist.
Publisher discovery keys include the publisher scope, and DHT records grant no permissions. Signatures only prove who made a claim; probing only checks availability.
Reputation is an opt-in service-quality ledger, disabled by default. It is not an infringement finding, abuse-report handling, or proof of legal compliance.

## 0. Core positioning

**Be a "mere conduit", not a "platform".** Every design trade-off serves one goal: keep a node legally as close as possible to
"a network pipe that passively and automatically transmits data", rather than "an intermediary platform that organizes, selects, or promotes model services".

---

## 1. Legal basis (official primary sources)

### 1.1 United States: DMCA §512(a) mere-conduit safe harbor

17 U.S.C. §512(a) protects transmitting, routing, and providing connections, plus intermediate transient storage during transmission.
All five conditions must be met **at the same time**:

1. The transmission is initiated by someone other than the service provider
2. It is carried out through an automatic technical process, without the provider selecting the material
3. The provider does not select the recipients (except as an automatic response to a request)
4. Intermediate copies are not accessible to anyone other than the intended recipients and are kept no longer than needed for the transmission
5. The material is transmitted without modification of its content

§512(i) adds a global threshold: the provider must adopt, inform users of, and **reasonably implement** a repeat-infringer policy,
and must accommodate standard technical measures. §512(m) explicitly does not require active monitoring.

Sources:
- https://uscode.house.gov/view.xhtml?req=(title:17%20section:512%20edition:prelim)
- https://www.copyright.gov/policy/section512/section-512-full-report.pdf

**Cautionary case**: in *BMG v. Cox Communications* (4th Cir.), Cox blocked large volumes of infringement notices and
enforced its own termination policy poorly, so it had not "reasonably implemented" a repeat-infringer policy and lost the safe harbor.
**The burden of proving safe-harbor eligibility is on the service provider.**
Source: https://www.ca4.uscourts.gov/Opinions/161972.P.pdf

### 1.2 United States: Grokster inducement (the top risk for P2P software)

*MGM v. Grokster*, 545 U.S. 913 (2005). The rule:
> One who distributes a device with the object of promoting its use to infringe copyright, as shown by clear expression
> or other affirmative steps taken to foster infringement, is liable for the resulting acts of infringement by third parties.

The Supreme Court weighed three groups of evidence. Each maps to behavior this project must avoid:

| Grokster's problematic behavior | This project's constraint |
|---|---|
| Actively courted Napster's infringing users | Never mention "bypass API limits", "free closed-source models", or "evade bans" in any promotion |
| Developed no filtering tools (strengthening the inference of bad intent given other evidence) | Ship built-in filtering rules and an abuse-report channel by default |
| Advertising revenue that depended on high traffic | No business model that takes a cut of relayed traffic |

Note: Sony's "substantial non-infringing uses" does not defend against **intentional inducement**. Conversely, merely providing neutral connectivity
while knowing that infringement exists on the network usually does not amount to inducement.
Source: https://www.govinfo.gov/content/pkg/USREPORTS-545/pdf/USREPORTS-545-913.pdf

### 1.3 China: Interim Measures for the Management of Generative AI Services

Order No. 15 of seven ministries, published 2023-07-10, effective 2023-08-15.

Article 17: providers of generative AI services **with public-opinion attributes or social-mobilization capability** must carry out a security assessment
and complete algorithm filing, amendment, and deregistration under the Provisions on the Administration of Algorithmic Recommendation of Internet Information Services.

Key boundaries:
- Internal R&D or internal use that **does not serve the public within China** is outside the scope of these Measures
- Organizations or individuals that provide services through an API **are "providers"**
- Filing deadlines: within 10 working days of starting service; amendments within 10 working days; deregistration within 20 working days
- Launched applications must prominently display the model name and filing number

**Do not cite the April 2023 draft for comment** (which required filing for every service); the final text is narrower.

Sources:
- https://www.cac.gov.cn/2023-07/13/c_1690898327029107.htm
- https://www.cac.gov.cn/2022-01/04/c_1642894606364259.htm

### 1.4 European Union: AI Act (Regulation (EU) 2024/1689)

Three roles. **Chapter V regulates only GPAI model providers; there is no "GPAI model deployer" obligation category**:

- GPAI model provider: develops a model and places it on the EU market under its own name
- AI system provider: integrates a model into its own branded product offered to others
- Deployer: uses an AI system in the course of a business activity, excluding purely personal non-professional use

Points relevant to this project:
- Using only third-party model APIs generally does **not** make you a GPAI model provider
- Fine-tuning does not automatically create a new provider; the indicative threshold is modification compute above 1/3 of the original training compute
- Article 50 transparency: users interacting directly with AI must be told it is AI; synthetic content must carry machine-readable marking
- Timeline: GPAI provider obligations apply from 2025-08-02, with full enforcement from 2026-08-02;
  Article 50 transparency obligations apply from 2026-08-02
- Penalties: breaches of Articles 16/26/50 up to EUR 15 million or 3% of worldwide annual turnover, whichever is higher

Sources:
- https://eur-lex.europa.eu/legal-content/EN/TXT/?uri=CELEX:32024R1689
- https://digital-strategy.ec.europa.eu/en/faqs/guidelines-obligations-general-purpose-ai-providers

### 1.5 Model licenses: Llama as an example

Each Meta Llama release uses a **custom Community License**, not Apache or MIT. Licenses are published per version
(Llama 3 / 3.1 / 3.2 / 3.3 / 4 are separate) and must not be mixed. Core obligations:

- Prominently display `Built with Meta Llama 3`
- Derivative model names must start with `Llama 3`
- Distributed copies must include the specified Notice
- **Do not use its materials or outputs to improve other large language models** (except Llama itself and derivatives)
- Entities with more than 700 million MAU on the release date must request a separate license

Prohibited uses in the AUP include illegal activity, weapons and ITAR-controlled items, malicious code, unauthorized impersonation,
unlicensed medical/legal/financial professional services, and passing off AI-generated content as human-created.

Sources:
- https://github.com/meta-llama/llama-models/blob/main/models/llama3/LICENSE
- https://github.com/meta-llama/llama-models/blob/main/models/llama3/USE_POLICY.md

Note: `ai.meta.com/llama/use-policy` still shows the Llama 2 version. Check the terms against the matching version directory
in the official `meta-llama/llama-models` repository.

---

## 2. Architectural liability isolation

### 2.1 Relay nodes strictly implement mere-conduit semantics

Hard code-level constraints mapped to the five §512(a) conditions:

```
Relay requirements (implemented by internal/network/node integrating libp2p Circuit Relay v2):
  ✅ Forward only; never parse prompt / completion content
  ✅ End-to-end encryption: relays never see plaintext (libp2p secure channel between endpoints; no extra application-layer key protocol)
  ✅ Nothing on disk: bounded memory buffers, released when the transfer completes; no log files
  ✅ No rewriting: byte-level passthrough; no injected system prompt, no content rewriting
  ✅ No selection: routing decisions are made by the requester's scheduler; relays only answer connection requests
```

Anti-patterns (these destroy the mere-conduit defense):
- ❌ Relay-side response caching for "acceleration" → violates condition 4 (transience)
- ❌ Relay-side prompt injection or rewriting → violates condition 5 (no modification)
- ❌ Relay-side logging of full conversations for "debugging" → violates condition 4 and creates evidentiary risk
- ❌ Relay-side content moderation → self-contradictory: claiming to be a passive conduit while actively selecting material

> Design tension (needs an explicit decision): content filtering and the mere-conduit defense conflict when placed in the **same layer**.
> The fix is **layering**: filter at the provider node's entry point (you are responsible for your own model),
> and keep the relay layer purely passive. §512(m) supports relays having no duty to monitor.

### 2.2 The discovery layer is not a "directory service"

The legacy private-network DHT used CIDs derived from model names; publisher mode uses versioned publisher/model discovery keys.
Hashing does not keep model names secret, and capability queries can still reveal model metadata.
Nodes do not run a centralized "model marketplace" website, avoiding an information location tool in the §512(d) sense
and the Grokster-style appearance of "organizing infringing content".

### 2.3 Signed capability announcements and audit trail

Capability announcements returned by `/noobloft/1.0.0/capability` must be signed with the provider's private key and include:

```
peerID, modelName, modelLicense, licenseAttribution,
quantization, contextLength, jurisdiction, aupVersion, timestamp, signature
```

The following are historical design goals. Field propagation and response-header behavior must be checked against the code and tests; they are not all implemented.
The purpose is threefold:
1. Place responsibility for "what model I claim to run" clearly on the provider, not on relays or the software authors
2. The `modelLicense` + `licenseAttribution` fields make Llama-style attribution machine-checkable;
   the consumer-side gateway automatically surfaces `Built with Meta Llama 3` in a response header
3. The trail stores only signatures and metadata, **never inference content**, balancing auditability with mere-conduit status

### 2.4 The §512(i) repeat-infringer policy must actually be enforced

These are historical requirements not yet implemented. The current reputation module only records service quality and does not implement these legal procedures:

- `internal/serving/reputation/`: abuse report → scoring → automatic addition to a local denylist when a threshold is reached
- Policy text and a contact email ship with the software and are shown on first start
- Termination records can be exported to prove "reasonable implementation"
- Thresholds and termination actions are **on by default**, with no one-click global off switch

### 2.5 Safe-by-default switches

| Switch | Default | Reason |
|---|---|---|
| Relay traffic for unknown peers | **Off** | Do not carry strangers' traffic by default; users opt in explicitly |
| Expose local models | **Off** | Avoid users unintentionally becoming "providers" |
| Local gateway bind address | `127.0.0.1` | Never default to `0.0.0.0` |
| Local gateway authentication | **On** (random token) | An unauthenticated HTTP gateway is an open proxy |
| Anonymous relays / onion routing | **Not implemented** | Strong anonymity would be read as evidence of intent to evade |

> The last row is a deliberate trade-off: multi-hop anonymity is technically possible, but it feeds directly into the Grokster
> inference of "affirmative steps". Traceable single-hop relaying is much safer legally.

---

## 3. Distribution-side liability avoidance

### 3.1 The software itself

- License: **Apache-2.0** (explicit patent grant and contributor disclaimer), with a `NOTICE` file
- The README's first screen states the purpose: **private networking and compute sharing on your own hardware**,
  never mentioning "access restricted models" or "bypass limits"
- Include `DISCLAIMER.md`: AS IS, no warranty, users are responsible for their own compliance
- Include `ACCEPTABLE_USE.md`, which cites and requires compliance with each connected model's AUP
- Ship no preset forwarding configuration or keys pointing to third-party commercial APIs

### 3.2 Operations (if you run your own bootstrap / public relay)

- Bootstrap nodes only do peer discovery, no content indexing, and keep minimal connection logs for forensic needs
- Register a DMCA agent: §512(a) itself does not require it, but once you also host a website, docs, or an index,
  that part falls under §512(c) and needs a separate assessment
- If you serve the public in mainland China and the service has public-opinion attributes or social-mobilization capability
  → security assessment + algorithm filing + prominent model name and filing number
- If you serve the EU: determine whether you are a deployer or a downstream AI system provider;
  Article 50 requires telling users in the interface that they are interacting with AI

### 3.3 Business-model red lines

Do not take a cut of relayed traffic. In Grokster, "more usage, more revenue" was used to infer
"intent to profit from the scale of infringement". Viable alternatives:
- Pure P2P reciprocal quotas (tit-for-tat bandwidth credits, no money involved)
- Subscription fees for private enterprise deployments (decoupled from traffic volume)

---

## 4. Implementation priority

| Order | Item | Blocks release |
|---|---|---|
| 1 | Code-level guarantees of the five relay conditions (nothing on disk / no rewriting / E2E encryption) | Yes |
| 2 | Local gateway defaults to 127.0.0.1 + token authentication | Yes |
| 3 | LICENSE / NOTICE / DISCLAIMER / ACCEPTABLE_USE | Yes |
| 4 | Signed capability announcements + license attribution fields propagated to response headers | Yes |
| 5 | Automatic scoring and termination for the repeat-infringer policy | Yes |
| 6 | Default switch orientation (relay off, model exposure off) | Yes |
| 7 | Attorney review for target markets (China / US / EU by actual operating region) | Before release |
| 8 | Filing and security assessment (only when serving the public in China and Article 17 applies) | As needed |

## 5. Open questions

1. Scope of operation: personal/internal networking only, or public-facing? This decides whether China's Article 17 applies
   and how the EU role is characterized. It is the largest fork point for every conclusion in this document.
2. Whether to run a public bootstrap / relay: operating one turns you from a "software author" into a "service provider",
   with a different order of liability.
3. Whether to support forwarding to closed-source commercial APIs: this introduces breach-of-ToS risk with each vendor
   (usually prohibiting resale and credential sharing), a risk layer independent of §512 and the AI Act.
   Recommendation: the first version **supports self-hosted open-source models only**.
