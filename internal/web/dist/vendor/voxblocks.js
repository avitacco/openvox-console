/**
 * @license
 * Copyright 2019 Google LLC
 * SPDX-License-Identifier: BSD-3-Clause
 */
const Ze = globalThis, kt = Ze.ShadowRoot && (Ze.ShadyCSS === void 0 || Ze.ShadyCSS.nativeShadow) && "adoptedStyleSheets" in Document.prototype && "replace" in CSSStyleSheet.prototype, Lt = Symbol(), Gt = /* @__PURE__ */ new WeakMap();
let lo = class {
  constructor(e, o, s) {
    if (this._$cssResult$ = !0, s !== Lt) throw Error("CSSResult is not constructable. Use `unsafeCSS` or `css` instead.");
    this.cssText = e, this.t = o;
  }
  get styleSheet() {
    let e = this.o;
    const o = this.t;
    if (kt && e === void 0) {
      const s = o !== void 0 && o.length === 1;
      s && (e = Gt.get(o)), e === void 0 && ((this.o = e = new CSSStyleSheet()).replaceSync(this.cssText), s && Gt.set(o, e));
    }
    return e;
  }
  toString() {
    return this.cssText;
  }
};
const Mo = (r) => new lo(typeof r == "string" ? r : r + "", void 0, Lt), d = (r, ...e) => {
  const o = r.length === 1 ? r[0] : e.reduce((s, t, a) => s + ((i) => {
    if (i._$cssResult$ === !0) return i.cssText;
    if (typeof i == "number") return i;
    throw Error("Value passed to 'css' function must be a 'css' function result: " + i + ". Use 'unsafeCSS' to pass non-literal values, but take care to ensure page security.");
  })(t) + r[a + 1], r[0]);
  return new lo(o, r, Lt);
}, Po = (r, e) => {
  if (kt) r.adoptedStyleSheets = e.map((o) => o instanceof CSSStyleSheet ? o : o.styleSheet);
  else for (const o of e) {
    const s = document.createElement("style"), t = Ze.litNonce;
    t !== void 0 && s.setAttribute("nonce", t), s.textContent = o.cssText, r.appendChild(s);
  }
}, Xt = kt ? (r) => r : (r) => r instanceof CSSStyleSheet ? ((e) => {
  let o = "";
  for (const s of e.cssRules) o += s.cssText;
  return Mo(o);
})(r) : r;
/**
 * @license
 * Copyright 2017 Google LLC
 * SPDX-License-Identifier: BSD-3-Clause
 */
const { is: Ao, defineProperty: ko, getOwnPropertyDescriptor: Lo, getOwnPropertyNames: Eo, getOwnPropertySymbols: So, getPrototypeOf: jo } = Object, M = globalThis, Jt = M.trustedTypes, Vo = Jt ? Jt.emptyScript : "", gt = M.reactiveElementPolyfillSupport, ie = (r, e) => r, Ke = { toAttribute(r, e) {
  switch (e) {
    case Boolean:
      r = r ? Vo : null;
      break;
    case Object:
    case Array:
      r = r == null ? r : JSON.stringify(r);
  }
  return r;
}, fromAttribute(r, e) {
  let o = r;
  switch (e) {
    case Boolean:
      o = r !== null;
      break;
    case Number:
      o = r === null ? null : Number(r);
      break;
    case Object:
    case Array:
      try {
        o = JSON.parse(r);
      } catch {
        o = null;
      }
  }
  return o;
} }, Et = (r, e) => !Ao(r, e), Yt = { attribute: !0, type: String, converter: Ke, reflect: !1, useDefault: !1, hasChanged: Et };
Symbol.metadata ?? (Symbol.metadata = Symbol("metadata")), M.litPropertyMetadata ?? (M.litPropertyMetadata = /* @__PURE__ */ new WeakMap());
let q = class extends HTMLElement {
  static addInitializer(e) {
    this._$Ei(), (this.l ?? (this.l = [])).push(e);
  }
  static get observedAttributes() {
    return this.finalize(), this._$Eh && [...this._$Eh.keys()];
  }
  static createProperty(e, o = Yt) {
    if (o.state && (o.attribute = !1), this._$Ei(), this.prototype.hasOwnProperty(e) && ((o = Object.create(o)).wrapped = !0), this.elementProperties.set(e, o), !o.noAccessor) {
      const s = Symbol(), t = this.getPropertyDescriptor(e, s, o);
      t !== void 0 && ko(this.prototype, e, t);
    }
  }
  static getPropertyDescriptor(e, o, s) {
    const { get: t, set: a } = Lo(this.prototype, e) ?? { get() {
      return this[o];
    }, set(i) {
      this[o] = i;
    } };
    return { get: t, set(i) {
      const u = t == null ? void 0 : t.call(this);
      a == null || a.call(this, i), this.requestUpdate(e, u, s);
    }, configurable: !0, enumerable: !0 };
  }
  static getPropertyOptions(e) {
    return this.elementProperties.get(e) ?? Yt;
  }
  static _$Ei() {
    if (this.hasOwnProperty(ie("elementProperties"))) return;
    const e = jo(this);
    e.finalize(), e.l !== void 0 && (this.l = [...e.l]), this.elementProperties = new Map(e.elementProperties);
  }
  static finalize() {
    if (this.hasOwnProperty(ie("finalized"))) return;
    if (this.finalized = !0, this._$Ei(), this.hasOwnProperty(ie("properties"))) {
      const o = this.properties, s = [...Eo(o), ...So(o)];
      for (const t of s) this.createProperty(t, o[t]);
    }
    const e = this[Symbol.metadata];
    if (e !== null) {
      const o = litPropertyMetadata.get(e);
      if (o !== void 0) for (const [s, t] of o) this.elementProperties.set(s, t);
    }
    this._$Eh = /* @__PURE__ */ new Map();
    for (const [o, s] of this.elementProperties) {
      const t = this._$Eu(o, s);
      t !== void 0 && this._$Eh.set(t, o);
    }
    this.elementStyles = this.finalizeStyles(this.styles);
  }
  static finalizeStyles(e) {
    const o = [];
    if (Array.isArray(e)) {
      const s = new Set(e.flat(1 / 0).reverse());
      for (const t of s) o.unshift(Xt(t));
    } else e !== void 0 && o.push(Xt(e));
    return o;
  }
  static _$Eu(e, o) {
    const s = o.attribute;
    return s === !1 ? void 0 : typeof s == "string" ? s : typeof e == "string" ? e.toLowerCase() : void 0;
  }
  constructor() {
    super(), this._$Ep = void 0, this.isUpdatePending = !1, this.hasUpdated = !1, this._$Em = null, this._$Ev();
  }
  _$Ev() {
    var e;
    this._$ES = new Promise((o) => this.enableUpdating = o), this._$AL = /* @__PURE__ */ new Map(), this._$E_(), this.requestUpdate(), (e = this.constructor.l) == null || e.forEach((o) => o(this));
  }
  addController(e) {
    var o;
    (this._$EO ?? (this._$EO = /* @__PURE__ */ new Set())).add(e), this.renderRoot !== void 0 && this.isConnected && ((o = e.hostConnected) == null || o.call(e));
  }
  removeController(e) {
    var o;
    (o = this._$EO) == null || o.delete(e);
  }
  _$E_() {
    const e = /* @__PURE__ */ new Map(), o = this.constructor.elementProperties;
    for (const s of o.keys()) this.hasOwnProperty(s) && (e.set(s, this[s]), delete this[s]);
    e.size > 0 && (this._$Ep = e);
  }
  createRenderRoot() {
    const e = this.shadowRoot ?? this.attachShadow(this.constructor.shadowRootOptions);
    return Po(e, this.constructor.elementStyles), e;
  }
  connectedCallback() {
    var e;
    this.renderRoot ?? (this.renderRoot = this.createRenderRoot()), this.enableUpdating(!0), (e = this._$EO) == null || e.forEach((o) => {
      var s;
      return (s = o.hostConnected) == null ? void 0 : s.call(o);
    });
  }
  enableUpdating(e) {
  }
  disconnectedCallback() {
    var e;
    (e = this._$EO) == null || e.forEach((o) => {
      var s;
      return (s = o.hostDisconnected) == null ? void 0 : s.call(o);
    });
  }
  attributeChangedCallback(e, o, s) {
    this._$AK(e, s);
  }
  _$ET(e, o) {
    var a;
    const s = this.constructor.elementProperties.get(e), t = this.constructor._$Eu(e, s);
    if (t !== void 0 && s.reflect === !0) {
      const i = (((a = s.converter) == null ? void 0 : a.toAttribute) !== void 0 ? s.converter : Ke).toAttribute(o, s.type);
      this._$Em = e, i == null ? this.removeAttribute(t) : this.setAttribute(t, i), this._$Em = null;
    }
  }
  _$AK(e, o) {
    var a, i;
    const s = this.constructor, t = s._$Eh.get(e);
    if (t !== void 0 && this._$Em !== t) {
      const u = s.getPropertyOptions(t), v = typeof u.converter == "function" ? { fromAttribute: u.converter } : ((a = u.converter) == null ? void 0 : a.fromAttribute) !== void 0 ? u.converter : Ke;
      this._$Em = t;
      const f = v.fromAttribute(o, u.type);
      this[t] = f ?? ((i = this._$Ej) == null ? void 0 : i.get(t)) ?? f, this._$Em = null;
    }
  }
  requestUpdate(e, o, s, t = !1, a) {
    var i;
    if (e !== void 0) {
      const u = this.constructor;
      if (t === !1 && (a = this[e]), s ?? (s = u.getPropertyOptions(e)), !((s.hasChanged ?? Et)(a, o) || s.useDefault && s.reflect && a === ((i = this._$Ej) == null ? void 0 : i.get(e)) && !this.hasAttribute(u._$Eu(e, s)))) return;
      this.C(e, o, s);
    }
    this.isUpdatePending === !1 && (this._$ES = this._$EP());
  }
  C(e, o, { useDefault: s, reflect: t, wrapped: a }, i) {
    s && !(this._$Ej ?? (this._$Ej = /* @__PURE__ */ new Map())).has(e) && (this._$Ej.set(e, i ?? o ?? this[e]), a !== !0 || i !== void 0) || (this._$AL.has(e) || (this.hasUpdated || s || (o = void 0), this._$AL.set(e, o)), t === !0 && this._$Em !== e && (this._$Eq ?? (this._$Eq = /* @__PURE__ */ new Set())).add(e));
  }
  async _$EP() {
    this.isUpdatePending = !0;
    try {
      await this._$ES;
    } catch (o) {
      Promise.reject(o);
    }
    const e = this.scheduleUpdate();
    return e != null && await e, !this.isUpdatePending;
  }
  scheduleUpdate() {
    return this.performUpdate();
  }
  performUpdate() {
    var s;
    if (!this.isUpdatePending) return;
    if (!this.hasUpdated) {
      if (this.renderRoot ?? (this.renderRoot = this.createRenderRoot()), this._$Ep) {
        for (const [a, i] of this._$Ep) this[a] = i;
        this._$Ep = void 0;
      }
      const t = this.constructor.elementProperties;
      if (t.size > 0) for (const [a, i] of t) {
        const { wrapped: u } = i, v = this[a];
        u !== !0 || this._$AL.has(a) || v === void 0 || this.C(a, void 0, i, v);
      }
    }
    let e = !1;
    const o = this._$AL;
    try {
      e = this.shouldUpdate(o), e ? (this.willUpdate(o), (s = this._$EO) == null || s.forEach((t) => {
        var a;
        return (a = t.hostUpdate) == null ? void 0 : a.call(t);
      }), this.update(o)) : this._$EM();
    } catch (t) {
      throw e = !1, this._$EM(), t;
    }
    e && this._$AE(o);
  }
  willUpdate(e) {
  }
  _$AE(e) {
    var o;
    (o = this._$EO) == null || o.forEach((s) => {
      var t;
      return (t = s.hostUpdated) == null ? void 0 : t.call(s);
    }), this.hasUpdated || (this.hasUpdated = !0, this.firstUpdated(e)), this.updated(e);
  }
  _$EM() {
    this._$AL = /* @__PURE__ */ new Map(), this.isUpdatePending = !1;
  }
  get updateComplete() {
    return this.getUpdateComplete();
  }
  getUpdateComplete() {
    return this._$ES;
  }
  shouldUpdate(e) {
    return !0;
  }
  update(e) {
    this._$Eq && (this._$Eq = this._$Eq.forEach((o) => this._$ET(o, this[o]))), this._$EM();
  }
  updated(e) {
  }
  firstUpdated(e) {
  }
};
q.elementStyles = [], q.shadowRootOptions = { mode: "open" }, q[ie("elementProperties")] = /* @__PURE__ */ new Map(), q[ie("finalized")] = /* @__PURE__ */ new Map(), gt == null || gt({ ReactiveElement: q }), (M.reactiveElementVersions ?? (M.reactiveElementVersions = [])).push("2.1.2");
/**
 * @license
 * Copyright 2017 Google LLC
 * SPDX-License-Identifier: BSD-3-Clause
 */
const ne = globalThis, Qt = (r) => r, We = ne.trustedTypes, eo = We ? We.createPolicy("lit-html", { createHTML: (r) => r }) : void 0, co = "$lit$", O = `lit$${Math.random().toFixed(9).slice(2)}$`, ho = "?" + O, Do = `<${ho}>`, j = document, le = () => j.createComment(""), ce = (r) => r === null || typeof r != "object" && typeof r != "function", St = Array.isArray, zo = (r) => St(r) || typeof (r == null ? void 0 : r[Symbol.iterator]) == "function", bt = `[ 	
\f\r]`, ae = /<(?:(!--|\/[^a-zA-Z])|(\/?[a-zA-Z][^>\s]*)|(\/?$))/g, to = /-->/g, oo = />/g, k = RegExp(`>|${bt}(?:([^\\s"'>=/]+)(${bt}*=${bt}*(?:[^ 	
\f\r"'\`<>=]|("|')|))|$)`, "g"), ro = /'/g, so = /"/g, po = /^(?:script|style|textarea|title)$/i, vo = (r) => (e, ...o) => ({ _$litType$: r, strings: e, values: o }), l = vo(1), c = vo(2), y = Symbol.for("lit-noChange"), x = Symbol.for("lit-nothing"), ao = /* @__PURE__ */ new WeakMap(), E = j.createTreeWalker(j, 129);
function uo(r, e) {
  if (!St(r) || !r.hasOwnProperty("raw")) throw Error("invalid template strings array");
  return eo !== void 0 ? eo.createHTML(e) : e;
}
const Ho = (r, e) => {
  const o = r.length - 1, s = [];
  let t, a = e === 2 ? "<svg>" : e === 3 ? "<math>" : "", i = ae;
  for (let u = 0; u < o; u++) {
    const v = r[u];
    let f, b, g = -1, w = 0;
    for (; w < v.length && (i.lastIndex = w, b = i.exec(v), b !== null); ) w = i.lastIndex, i === ae ? b[1] === "!--" ? i = to : b[1] !== void 0 ? i = oo : b[2] !== void 0 ? (po.test(b[2]) && (t = RegExp("</" + b[2], "g")), i = k) : b[3] !== void 0 && (i = k) : i === k ? b[0] === ">" ? (i = t ?? ae, g = -1) : b[1] === void 0 ? g = -2 : (g = i.lastIndex - b[2].length, f = b[1], i = b[3] === void 0 ? k : b[3] === '"' ? so : ro) : i === so || i === ro ? i = k : i === to || i === oo ? i = ae : (i = k, t = void 0);
    const C = i === k && r[u + 1].startsWith("/>") ? " " : "";
    a += i === ae ? v + Do : g >= 0 ? (s.push(f), v.slice(0, g) + co + v.slice(g) + O + C) : v + O + (g === -2 ? u : C);
  }
  return [uo(r, a + (r[o] || "<?>") + (e === 2 ? "</svg>" : e === 3 ? "</math>" : "")), s];
};
class de {
  constructor({ strings: e, _$litType$: o }, s) {
    let t;
    this.parts = [];
    let a = 0, i = 0;
    const u = e.length - 1, v = this.parts, [f, b] = Ho(e, o);
    if (this.el = de.createElement(f, s), E.currentNode = this.el.content, o === 2 || o === 3) {
      const g = this.el.content.firstChild;
      g.replaceWith(...g.childNodes);
    }
    for (; (t = E.nextNode()) !== null && v.length < u; ) {
      if (t.nodeType === 1) {
        if (t.hasAttributes()) for (const g of t.getAttributeNames()) if (g.endsWith(co)) {
          const w = b[i++], C = t.getAttribute(g).split(O), qe = /([.?@])?(.*)/.exec(w);
          v.push({ type: 1, index: a, name: qe[2], strings: C, ctor: qe[1] === "." ? Bo : qe[1] === "?" ? No : qe[1] === "@" ? Ro : it }), t.removeAttribute(g);
        } else g.startsWith(O) && (v.push({ type: 6, index: a }), t.removeAttribute(g));
        if (po.test(t.tagName)) {
          const g = t.textContent.split(O), w = g.length - 1;
          if (w > 0) {
            t.textContent = We ? We.emptyScript : "";
            for (let C = 0; C < w; C++) t.append(g[C], le()), E.nextNode(), v.push({ type: 2, index: ++a });
            t.append(g[w], le());
          }
        }
      } else if (t.nodeType === 8) if (t.data === ho) v.push({ type: 2, index: a });
      else {
        let g = -1;
        for (; (g = t.data.indexOf(O, g + 1)) !== -1; ) v.push({ type: 7, index: a }), g += O.length - 1;
      }
      a++;
    }
  }
  static createElement(e, o) {
    const s = j.createElement("template");
    return s.innerHTML = e, s;
  }
}
function F(r, e, o = r, s) {
  var i, u;
  if (e === y) return e;
  let t = s !== void 0 ? (i = o._$Co) == null ? void 0 : i[s] : o._$Cl;
  const a = ce(e) ? void 0 : e._$litDirective$;
  return (t == null ? void 0 : t.constructor) !== a && ((u = t == null ? void 0 : t._$AO) == null || u.call(t, !1), a === void 0 ? t = void 0 : (t = new a(r), t._$AT(r, o, s)), s !== void 0 ? (o._$Co ?? (o._$Co = []))[s] = t : o._$Cl = t), t !== void 0 && (e = F(r, t._$AS(r, e.values), t, s)), e;
}
class To {
  constructor(e, o) {
    this._$AV = [], this._$AN = void 0, this._$AD = e, this._$AM = o;
  }
  get parentNode() {
    return this._$AM.parentNode;
  }
  get _$AU() {
    return this._$AM._$AU;
  }
  u(e) {
    const { el: { content: o }, parts: s } = this._$AD, t = ((e == null ? void 0 : e.creationScope) ?? j).importNode(o, !0);
    E.currentNode = t;
    let a = E.nextNode(), i = 0, u = 0, v = s[0];
    for (; v !== void 0; ) {
      if (i === v.index) {
        let f;
        v.type === 2 ? f = new Se(a, a.nextSibling, this, e) : v.type === 1 ? f = new v.ctor(a, v.name, v.strings, this, e) : v.type === 6 && (f = new Io(a, this, e)), this._$AV.push(f), v = s[++u];
      }
      i !== (v == null ? void 0 : v.index) && (a = E.nextNode(), i++);
    }
    return E.currentNode = j, t;
  }
  p(e) {
    let o = 0;
    for (const s of this._$AV) s !== void 0 && (s.strings !== void 0 ? (s._$AI(e, s, o), o += s.strings.length - 2) : s._$AI(e[o])), o++;
  }
}
class Se {
  get _$AU() {
    var e;
    return ((e = this._$AM) == null ? void 0 : e._$AU) ?? this._$Cv;
  }
  constructor(e, o, s, t) {
    this.type = 2, this._$AH = x, this._$AN = void 0, this._$AA = e, this._$AB = o, this._$AM = s, this.options = t, this._$Cv = (t == null ? void 0 : t.isConnected) ?? !0;
  }
  get parentNode() {
    let e = this._$AA.parentNode;
    const o = this._$AM;
    return o !== void 0 && (e == null ? void 0 : e.nodeType) === 11 && (e = o.parentNode), e;
  }
  get startNode() {
    return this._$AA;
  }
  get endNode() {
    return this._$AB;
  }
  _$AI(e, o = this) {
    e = F(this, e, o), ce(e) ? e === x || e == null || e === "" ? (this._$AH !== x && this._$AR(), this._$AH = x) : e !== this._$AH && e !== y && this._(e) : e._$litType$ !== void 0 ? this.$(e) : e.nodeType !== void 0 ? this.T(e) : zo(e) ? this.k(e) : this._(e);
  }
  O(e) {
    return this._$AA.parentNode.insertBefore(e, this._$AB);
  }
  T(e) {
    this._$AH !== e && (this._$AR(), this._$AH = this.O(e));
  }
  _(e) {
    this._$AH !== x && ce(this._$AH) ? this._$AA.nextSibling.data = e : this.T(j.createTextNode(e)), this._$AH = e;
  }
  $(e) {
    var a;
    const { values: o, _$litType$: s } = e, t = typeof s == "number" ? this._$AC(e) : (s.el === void 0 && (s.el = de.createElement(uo(s.h, s.h[0]), this.options)), s);
    if (((a = this._$AH) == null ? void 0 : a._$AD) === t) this._$AH.p(o);
    else {
      const i = new To(t, this), u = i.u(this.options);
      i.p(o), this.T(u), this._$AH = i;
    }
  }
  _$AC(e) {
    let o = ao.get(e.strings);
    return o === void 0 && ao.set(e.strings, o = new de(e)), o;
  }
  k(e) {
    St(this._$AH) || (this._$AH = [], this._$AR());
    const o = this._$AH;
    let s, t = 0;
    for (const a of e) t === o.length ? o.push(s = new Se(this.O(le()), this.O(le()), this, this.options)) : s = o[t], s._$AI(a), t++;
    t < o.length && (this._$AR(s && s._$AB.nextSibling, t), o.length = t);
  }
  _$AR(e = this._$AA.nextSibling, o) {
    var s;
    for ((s = this._$AP) == null ? void 0 : s.call(this, !1, !0, o); e !== this._$AB; ) {
      const t = Qt(e).nextSibling;
      Qt(e).remove(), e = t;
    }
  }
  setConnected(e) {
    var o;
    this._$AM === void 0 && (this._$Cv = e, (o = this._$AP) == null || o.call(this, e));
  }
}
class it {
  get tagName() {
    return this.element.tagName;
  }
  get _$AU() {
    return this._$AM._$AU;
  }
  constructor(e, o, s, t, a) {
    this.type = 1, this._$AH = x, this._$AN = void 0, this.element = e, this.name = o, this._$AM = t, this.options = a, s.length > 2 || s[0] !== "" || s[1] !== "" ? (this._$AH = Array(s.length - 1).fill(new String()), this.strings = s) : this._$AH = x;
  }
  _$AI(e, o = this, s, t) {
    const a = this.strings;
    let i = !1;
    if (a === void 0) e = F(this, e, o, 0), i = !ce(e) || e !== this._$AH && e !== y, i && (this._$AH = e);
    else {
      const u = e;
      let v, f;
      for (e = a[0], v = 0; v < a.length - 1; v++) f = F(this, u[s + v], o, v), f === y && (f = this._$AH[v]), i || (i = !ce(f) || f !== this._$AH[v]), f === x ? e = x : e !== x && (e += (f ?? "") + a[v + 1]), this._$AH[v] = f;
    }
    i && !t && this.j(e);
  }
  j(e) {
    e === x ? this.element.removeAttribute(this.name) : this.element.setAttribute(this.name, e ?? "");
  }
}
class Bo extends it {
  constructor() {
    super(...arguments), this.type = 3;
  }
  j(e) {
    this.element[this.name] = e === x ? void 0 : e;
  }
}
class No extends it {
  constructor() {
    super(...arguments), this.type = 4;
  }
  j(e) {
    this.element.toggleAttribute(this.name, !!e && e !== x);
  }
}
class Ro extends it {
  constructor(e, o, s, t, a) {
    super(e, o, s, t, a), this.type = 5;
  }
  _$AI(e, o = this) {
    if ((e = F(this, e, o, 0) ?? x) === y) return;
    const s = this._$AH, t = e === x && s !== x || e.capture !== s.capture || e.once !== s.once || e.passive !== s.passive, a = e !== x && (s === x || t);
    t && this.element.removeEventListener(this.name, this, s), a && this.element.addEventListener(this.name, this, e), this._$AH = e;
  }
  handleEvent(e) {
    var o;
    typeof this._$AH == "function" ? this._$AH.call(((o = this.options) == null ? void 0 : o.host) ?? this.element, e) : this._$AH.handleEvent(e);
  }
}
class Io {
  constructor(e, o, s) {
    this.element = e, this.type = 6, this._$AN = void 0, this._$AM = o, this.options = s;
  }
  get _$AU() {
    return this._$AM._$AU;
  }
  _$AI(e) {
    F(this, e);
  }
}
const mt = ne.litHtmlPolyfillSupport;
mt == null || mt(de, Se), (ne.litHtmlVersions ?? (ne.litHtmlVersions = [])).push("3.3.3");
const Uo = (r, e, o) => {
  const s = (o == null ? void 0 : o.renderBefore) ?? e;
  let t = s._$litPart$;
  if (t === void 0) {
    const a = (o == null ? void 0 : o.renderBefore) ?? null;
    s._$litPart$ = t = new Se(e.insertBefore(le(), a), a, void 0, o ?? {});
  }
  return t._$AI(r), t;
};
/**
 * @license
 * Copyright 2017 Google LLC
 * SPDX-License-Identifier: BSD-3-Clause
 */
const S = globalThis;
let p = class extends q {
  constructor() {
    super(...arguments), this.renderOptions = { host: this }, this._$Do = void 0;
  }
  createRenderRoot() {
    var o;
    const e = super.createRenderRoot();
    return (o = this.renderOptions).renderBefore ?? (o.renderBefore = e.firstChild), e;
  }
  update(e) {
    const o = this.render();
    this.hasUpdated || (this.renderOptions.isConnected = this.isConnected), super.update(e), this._$Do = Uo(o, this.renderRoot, this.renderOptions);
  }
  connectedCallback() {
    var e;
    super.connectedCallback(), (e = this._$Do) == null || e.setConnected(!0);
  }
  disconnectedCallback() {
    var e;
    super.disconnectedCallback(), (e = this._$Do) == null || e.setConnected(!1);
  }
  render() {
    return y;
  }
};
var no;
p._$litElement$ = !0, p.finalized = !0, (no = S.litElementHydrateSupport) == null || no.call(S, { LitElement: p });
const yt = S.litElementPolyfillSupport;
yt == null || yt({ LitElement: p });
(S.litElementVersions ?? (S.litElementVersions = [])).push("4.2.2");
/**
 * @license
 * Copyright 2017 Google LLC
 * SPDX-License-Identifier: BSD-3-Clause
 */
const h = (r) => (e, o) => {
  o !== void 0 ? o.addInitializer(() => {
    customElements.define(r, e);
  }) : customElements.define(r, e);
};
/**
 * @license
 * Copyright 2017 Google LLC
 * SPDX-License-Identifier: BSD-3-Clause
 */
const qo = { attribute: !0, type: String, converter: Ke, reflect: !1, hasChanged: Et }, Fo = (r = qo, e, o) => {
  const { kind: s, metadata: t } = o;
  let a = globalThis.litPropertyMetadata.get(t);
  if (a === void 0 && globalThis.litPropertyMetadata.set(t, a = /* @__PURE__ */ new Map()), s === "setter" && ((r = Object.create(r)).wrapped = !0), a.set(o.name, r), s === "accessor") {
    const { name: i } = o;
    return { set(u) {
      const v = e.get.call(this);
      e.set.call(this, u), this.requestUpdate(i, v, r, !0, u);
    }, init(u) {
      return u !== void 0 && this.C(i, void 0, r, u), u;
    } };
  }
  if (s === "setter") {
    const { name: i } = o;
    return function(u) {
      const v = this[i];
      e.call(this, u), this.requestUpdate(i, v, r, !0, u);
    };
  }
  throw Error("Unsupported decorator location: " + s);
};
function n(r) {
  return (e, o) => typeof o == "object" ? Fo(r, e, o) : ((s, t, a) => {
    const i = t.hasOwnProperty(a);
    return t.constructor.createProperty(a, s), i ? Object.getOwnPropertyDescriptor(t, a) : void 0;
  })(r, e, o);
}
/**
 * @license
 * Copyright 2017 Google LLC
 * SPDX-License-Identifier: BSD-3-Clause
 */
function nt(r) {
  return n({ ...r, state: !0, attribute: !1 });
}
/**
 * @license
 * Copyright 2017 Google LLC
 * SPDX-License-Identifier: BSD-3-Clause
 */
const Zo = (r, e, o) => (o.configurable = !0, o.enumerable = !0, Reflect.decorate && typeof e != "object" && Object.defineProperty(r, e, o), o);
/**
 * @license
 * Copyright 2017 Google LLC
 * SPDX-License-Identifier: BSD-3-Clause
 */
function jt(r, e) {
  return (o, s, t) => {
    const a = (i) => {
      var u;
      return ((u = i.renderRoot) == null ? void 0 : u.querySelector(r)) ?? null;
    };
    return Zo(o, s, { get() {
      return a(this);
    } });
  };
}
/**
 * @license
 * Copyright 2017 Google LLC
 * SPDX-License-Identifier: BSD-3-Clause
 */
const L = { ATTRIBUTE: 1, PROPERTY: 3, BOOLEAN_ATTRIBUTE: 4 }, xo = (r) => (...e) => ({ _$litDirective$: r, values: e });
class fo {
  constructor(e) {
  }
  get _$AU() {
    return this._$AM._$AU;
  }
  _$AT(e, o, s) {
    this._$Ct = e, this._$AM = o, this._$Ci = s;
  }
  _$AS(e, o) {
    return this.update(e, o);
  }
  update(e, o) {
    return this.render(...o);
  }
}
/**
 * @license
 * Copyright 2018 Google LLC
 * SPDX-License-Identifier: BSD-3-Clause
 */
const Ko = xo(class extends fo {
  constructor(r) {
    var e;
    if (super(r), r.type !== L.ATTRIBUTE || r.name !== "class" || ((e = r.strings) == null ? void 0 : e.length) > 2) throw Error("`classMap()` can only be used in the `class` attribute and must be the only part in the attribute.");
  }
  render(r) {
    return " " + Object.keys(r).filter((e) => r[e]).join(" ") + " ";
  }
  update(r, [e]) {
    var s, t;
    if (this.st === void 0) {
      this.st = /* @__PURE__ */ new Set(), r.strings !== void 0 && (this.nt = new Set(r.strings.join(" ").split(/\s/).filter((a) => a !== "")));
      for (const a in e) e[a] && !((s = this.nt) != null && s.has(a)) && this.st.add(a);
      return this.render(e);
    }
    const o = r.element.classList;
    for (const a of this.st) a in e || (o.remove(a), this.st.delete(a));
    for (const a in e) {
      const i = !!e[a];
      i === this.st.has(a) || (t = this.nt) != null && t.has(a) || (i ? (o.add(a), this.st.add(a)) : (o.remove(a), this.st.delete(a)));
    }
    return y;
  }
});
/**
 * @license
 * Copyright 2018 Google LLC
 * SPDX-License-Identifier: BSD-3-Clause
 */
const Z = (r) => r ?? x;
var Wo = Object.defineProperty, Go = Object.getOwnPropertyDescriptor, R = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? Go(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && Wo(e, o, t), t;
};
let _ = class extends p {
  constructor() {
    super(...arguments), this.variant = "brand", this.size = "md", this.type = "button", this.disabled = !1;
  }
  render() {
    const r = Ko({
      button: !0,
      [this.variant]: !0,
      [this.size]: !0
    });
    return this.href !== void 0 && !this.disabled ? l`
        <a
          class=${r}
          href=${this.href}
          target=${Z(this.target)}
          rel=${Z(this.target === "_blank" ? "noreferrer" : void 0)}
        >
          <slot></slot>
        </a>
      ` : l`
      <button class=${r} type=${this.type} ?disabled=${this.disabled}>
        <slot></slot>
      </button>
    `;
  }
};
_.styles = d`
    :host {
      display: inline-block;
    }

    :host([disabled]) {
      pointer-events: none;
    }

    .button {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      gap: var(--vox-space-2);
      border: 1px solid transparent;
      border-radius: var(--vox-radius-full);
      font-family: var(--vox-font-family-base);
      font-weight: 600;
      line-height: 1;
      text-decoration: none;
      cursor: pointer;
      white-space: nowrap;
      transition:
        color var(--vox-transition-base),
        background-color var(--vox-transition-base),
        border-color var(--vox-transition-base);
    }

    .button:focus-visible {
      outline: 2px solid var(--vox-color-brand-1);
      outline-offset: 2px;
    }

    .button:disabled {
      opacity: 0.5;
      cursor: not-allowed;
    }

    .sm {
      font-size: 12px;
      padding: 0 var(--vox-space-3);
      height: 28px;
    }

    .md {
      font-size: 14px;
      padding: 0 20px;
      height: 38px;
    }

    .lg {
      font-size: 16px;
      padding: 0 var(--vox-space-6);
      height: 48px;
    }

    .brand {
      background-color: var(--vox-color-brand-3);
      color: var(--vox-color-text-inverse);
    }

    .brand:hover:not(:disabled) {
      background-color: var(--vox-color-brand-2);
    }

    .alt {
      background-color: var(--vox-color-bg-soft);
      color: var(--vox-color-text-1);
      border-color: var(--vox-color-divider);
    }

    .alt:hover:not(:disabled) {
      border-color: var(--vox-color-brand-1);
      color: var(--vox-color-brand-1);
    }

    .danger {
      background-color: var(--vox-color-danger-3);
      color: var(--vox-color-text-inverse);
    }

    .danger:hover:not(:disabled) {
      background-color: var(--vox-color-danger-2);
    }

    .ghost {
      background-color: transparent;
      color: var(--vox-color-brand-1);
    }

    .ghost:hover:not(:disabled) {
      background-color: var(--vox-color-brand-soft);
    }

    /* Corner flattening when placed inside a <vox-input-group>. */
    :host([data-vox-group]) .button {
      height: 100%;
      border-radius: 0;
    }

    :host([data-vox-group='start']) .button {
      border-radius: var(--vox-radius-md) 0 0 var(--vox-radius-md);
    }

    :host([data-vox-group='end']) .button {
      border-radius: 0 var(--vox-radius-md) var(--vox-radius-md) 0;
    }
  `;
R([
  n()
], _.prototype, "variant", 2);
R([
  n()
], _.prototype, "size", 2);
R([
  n()
], _.prototype, "href", 2);
R([
  n()
], _.prototype, "target", 2);
R([
  n()
], _.prototype, "type", 2);
R([
  n({ type: Boolean, reflect: !0 })
], _.prototype, "disabled", 2);
_ = R([
  h("vox-button")
], _);
var Xo = Object.defineProperty, Jo = Object.getOwnPropertyDescriptor, Vt = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? Jo(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && Xo(e, o, t), t;
};
let he = class extends p {
  constructor() {
    super(...arguments), this.href = "#";
  }
  render() {
    return l`
      <a href=${this.href} target=${this.target ?? ""}>
        <slot></slot>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="M5 12h14" />
          <path d="m13 6 6 6-6 6" />
        </svg>
      </a>
    `;
  }
};
he.styles = d`
    :host {
      display: inline-block;
      font-family: var(--vox-font-family-base);
    }

    a {
      display: inline-flex;
      align-items: center;
      gap: var(--vox-space-2);
      color: var(--vox-color-brand-1);
      font-size: 15px;
      font-weight: 600;
      text-decoration: none;
    }

    a:hover {
      color: var(--vox-color-brand-2);
      text-decoration: underline;
    }

    a:focus-visible {
      outline: 2px solid var(--vox-color-brand-1);
      outline-offset: 2px;
      border-radius: var(--vox-radius-sm);
    }

    svg {
      width: 16px;
      height: 16px;
      transition: transform var(--vox-transition-fast);
    }

    a:hover svg {
      transform: translateX(3px);
    }
  `;
Vt([
  n()
], he.prototype, "href", 2);
Vt([
  n()
], he.prototype, "target", 2);
he = Vt([
  h("vox-cta")
], he);
var Yo = Object.defineProperty, Qo = Object.getOwnPropertyDescriptor, lt = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? Qo(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && Yo(e, o, t), t;
};
const io = "vox-theme", Fe = "data-vox-theme";
let K = class extends p {
  constructor() {
    super(...arguments), this.lightLabel = "Switch to dark theme", this.darkLabel = "Switch to light theme", this.dark = !1, this.media = window.matchMedia("(prefers-color-scheme: dark)"), this.handleSystemChange = (r) => {
      localStorage.getItem(io) || document.documentElement.setAttribute(Fe, r.matches ? "dark" : "light");
    };
  }
  connectedCallback() {
    super.connectedCallback(), this.syncFromDocument(), this.observer = new MutationObserver(() => this.syncFromDocument()), this.observer.observe(document.documentElement, {
      attributes: !0,
      attributeFilter: [Fe]
    }), this.media.addEventListener("change", this.handleSystemChange);
  }
  disconnectedCallback() {
    var r;
    super.disconnectedCallback(), (r = this.observer) == null || r.disconnect(), this.media.removeEventListener("change", this.handleSystemChange);
  }
  syncFromDocument() {
    const r = document.documentElement.getAttribute(Fe) === "dark";
    this.dark = r, this.toggleAttribute("dark", r);
  }
  handleClick() {
    const r = this.dark ? "light" : "dark";
    document.documentElement.setAttribute(Fe, r), localStorage.setItem(io, r), this.dispatchEvent(
      new CustomEvent("vox-theme-change", {
        detail: { theme: r },
        bubbles: !0,
        composed: !0
      })
    );
  }
  render() {
    const r = this.dark ? this.darkLabel : this.lightLabel;
    return l`
      <button
        type="button"
        role="switch"
        aria-checked=${this.dark}
        aria-label=${r}
        title=${r}
        @click=${this.handleClick}
      >
        <span class="track">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <circle cx="12" cy="12" r="4" />
            <path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41" />
          </svg>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79Z" />
          </svg>
          <span class="thumb"></span>
        </span>
      </button>
    `;
  }
};
K.styles = d`
    :host {
      display: inline-flex;
    }

    button {
      display: inline-flex;
      align-items: center;
      border: none;
      background: none;
      padding: 0;
      cursor: pointer;
      -webkit-tap-highlight-color: transparent;
    }

    button:focus-visible {
      outline: 2px solid var(--vox-color-brand-1);
      outline-offset: 2px;
      border-radius: var(--vox-radius-full);
    }

    .track {
      position: relative;
      display: inline-flex;
      align-items: center;
      justify-content: space-between;
      box-sizing: border-box;
      width: 44px;
      height: 24px;
      padding: 0 5px;
      border-radius: var(--vox-radius-full);
      background-color: var(--vox-color-bg-soft);
      border: 1px solid var(--vox-color-divider);
    }

    .track svg {
      position: relative;
      width: 13px;
      height: 13px;
      flex: none;
      color: var(--vox-color-text-3);
    }

    .thumb {
      position: absolute;
      top: 1px;
      left: 1px;
      width: 20px;
      height: 20px;
      border-radius: 50%;
      background-color: var(--vox-color-bg-elv);
      box-shadow: var(--vox-shadow-1);
      transition: transform var(--vox-transition-base);
    }

    :host([dark]) .thumb {
      transform: translateX(20px);
    }
  `;
lt([
  n({ attribute: "light-label" })
], K.prototype, "lightLabel", 2);
lt([
  n({ attribute: "dark-label" })
], K.prototype, "darkLabel", 2);
lt([
  nt()
], K.prototype, "dark", 2);
K = lt([
  h("vox-theme-toggle")
], K);
const V = {
  // Phase 1: shared UI baseline -----------------------------------------
  search: c`<circle cx="21" cy="21" r="13"/><path d="M30.5 30.5 L41 41"/>`,
  close: c`<path d="M14 14 L34 34 M34 14 L14 34"/>`,
  menu: c`<path d="M8 14 H40 M8 24 H40 M8 34 H40"/>`,
  "chevron-up": c`<path d="M14 28 L24 18 L34 28"/>`,
  "chevron-down": c`<path d="M14 20 L24 30 L34 20"/>`,
  "chevron-left": c`<path d="M28 14 L18 24 L28 34"/>`,
  "chevron-right": c`<path d="M20 14 L30 24 L20 34"/>`,
  "external-link": c`<path d="M36 26 V38 A4 4 0 0 1 32 42 H10 A4 4 0 0 1 6 38 V16 A4 4 0 0 1 10 12 H22"/><path d="M30 6 H42 V18"/><path d="M20 28 L42 6"/>`,
  copy: c`<rect x="8" y="14" width="24" height="24" rx="3"/><path d="M16 14 V10 A2 2 0 0 1 18 8 H38 A2 2 0 0 1 40 10 V30 A2 2 0 0 1 38 32 H34"/>`,
  check: c`<path d="M10 25 L20 35 L38 13"/>`,
  warning: c`<path d="M24 8 L44 40 H4 Z"/><path d="M24 20 V28"/><circle cx="24" cy="34" r="1.6" fill="currentColor" stroke="none"/>`,
  info: c`<circle cx="24" cy="24" r="17"/><path d="M24 22 V33"/><circle cx="24" cy="15.5" r="1.8" fill="currentColor" stroke="none"/>`,
  add: c`<path d="M24 10 V38 M10 24 H38"/>`,
  remove: c`<path d="M10 24 H38"/>`,
  edit: c`<path d="M30 8 L40 18 L18 40 L7 41 L8 30 Z"/><path d="M26 12 L36 22"/>`,
  delete: c`<path d="M10 14 H38"/><path d="M17 14 V9 A2 2 0 0 1 19 7 H29 A2 2 0 0 1 31 9 V14"/><path d="M13 14 L15 40 A2 2 0 0 0 17 42 H31 A2 2 0 0 0 33 40 L35 14"/><path d="M20 21 V35 M28 21 V35"/>`,
  filter: c`<path d="M6 10 H42 L28 26 V38 L20 42 V26 Z"/>`,
  sort: c`<path d="M14 30 V10 M14 10 L8 16 M14 10 L20 16"/><path d="M34 18 V38 M34 38 L28 32 M34 38 L40 32"/>`,
  calendar: c`<rect x="6" y="10" width="36" height="32" rx="3"/><path d="M6 20 H42"/><path d="M15 6 V14 M33 6 V14"/><circle cx="15" cy="28" r="1.6" fill="currentColor" stroke="none"/><circle cx="24" cy="28" r="1.6" fill="currentColor" stroke="none"/><circle cx="33" cy="28" r="1.6" fill="currentColor" stroke="none"/>`,
  clock: c`<circle cx="24" cy="24" r="17"/><path d="M24 14 V24 L32 29"/>`,
  person: c`<circle cx="24" cy="16" r="8"/><path d="M8 42 C8 31 15 26 24 26 C33 26 40 31 40 42"/>`,
  people: c`<circle cx="17" cy="16" r="7"/><circle cx="33" cy="18" r="6"/><path d="M4 41 C4 31 10 27 17 27 C21 27 24 28.3 26.3 30.5"/><path d="M24 41 C24 32 29 28 37 28 C43 28 44 32 44 41"/>`,
  lock: c`<rect x="10" y="21" width="28" height="21" rx="3"/><path d="M16 21 V15 A8 8 0 0 1 32 15 V21"/><circle cx="24" cy="31" r="2.2" fill="currentColor" stroke="none"/>`,
  eye: c`<path d="M4 24 C10 12 20 8 24 8 C28 8 38 12 44 24 C38 36 28 40 24 40 C20 40 10 36 4 24 Z"/><circle cx="24" cy="24" r="6"/>`,
  "eye-slash": c`<path d="M4 24 C10 12 20 8 24 8 C28 8 38 12 44 24 C38 36 28 40 24 40 C20 40 10 36 4 24 Z"/><circle cx="24" cy="24" r="6"/><path d="M6 6 L42 42"/>`,
  refresh: c`<path d="M46 8 L46 20 L34 20"/><path d="M2 40 L2 28 L14 28"/><path d="M7.02 18 A18 18 0 0 1 36.72 11.28 L46 20"/><path d="M2 28 L11.28 36.72 A18 18 0 0 0 40.98 30"/>`,
  // Phase 2: OpenVox marketing site --------------------------------------
  community: c`<circle cx="24" cy="10" r="5"/><circle cx="10" cy="34" r="5"/><circle cx="38" cy="34" r="5"/><path d="M24 15 L14 30 M24 15 L34 30 M15 34 H33"/>`,
  book: c`<path d="M24 12 C20 8 12 7 6 9 V37 C12 35 20 36 24 40 C28 36 36 35 42 37 V9 C36 7 28 8 24 12 Z"/><path d="M24 12 V40"/>`,
  terminal: c`<rect x="6" y="9" width="36" height="30" rx="3"/><path d="M14 19 L21 24 L14 29"/><path d="M25 30 H33"/>`,
  shield: c`<path d="M24 6 L40 12 V22 C40 33 33 40 24 43 C15 40 8 33 8 22 V12 Z"/><path d="M17 23 L22 28 L32 17"/>`,
  roadmap: c`<path d="M6 38 C14 38 14 26 22 26 C30 26 30 14 38 14"/><circle cx="6" cy="38" r="3" fill="currentColor" stroke="none"/><circle cx="22" cy="26" r="3" fill="currentColor" stroke="none"/><circle cx="38" cy="14" r="3" fill="currentColor" stroke="none"/>`,
  help: c`<circle cx="24" cy="24" r="17"/><path d="M18 18 C18 13 30 13 30 19 C30 24 24 23 24 29"/><circle cx="24" cy="35" r="1.8" fill="currentColor" stroke="none"/>`,
  repository: c`<rect x="7" y="10" width="34" height="28" rx="3"/><path d="M18 20 L13 24 L18 28 M30 20 L35 24 L30 28"/>`,
  star: c`<path d="M24 6 L29 19 L43 19 L32 28 L36 42 L24 34 L12 42 L16 28 L5 19 L19 19 Z"/>`,
  chat: c`<path d="M8 10 H40 A2 2 0 0 1 42 12 V30 A2 2 0 0 1 40 32 H20 L12 40 V32 H8 A2 2 0 0 1 6 30 V12 A2 2 0 0 1 8 10 Z"/><path d="M14 18 H34 M14 24 H28"/>`,
  heart: c`<path d="M24 41 C10 32 4 23 4 15.5 C4 9 9 5 15 5 C19.5 5 22.5 7.5 24 11 C25.5 7.5 28.5 5 33 5 C39 5 44 9 44 15.5 C44 23 38 32 24 41 Z"/>`,
  // Phase 3: module registry (Forge replacement) -------------------------
  module: c`<rect x="8" y="16" width="32" height="24" rx="3"/><path d="M8 24 H40"/><path d="M20 16 L24 24 L28 16"/>`,
  tag: c`<path d="M6 10 H26 L42 26 L26 42 H6 Z"/><circle cx="15" cy="19" r="3" fill="currentColor" stroke="none"/>`,
  dependency: c`<rect x="6" y="19" width="20" height="10" rx="5"/><rect x="22" y="19" width="20" height="10" rx="5"/>`,
  verified: c`<path d="M24 5 L31 10 L39 9 L40 17 L46 24 L40 31 L39 39 L31 38 L24 43 L17 38 L9 39 L8 31 L2 24 L8 17 L9 9 L17 10 Z"/><path d="M16 24 L21 29 L32 18"/>`,
  archived: c`<rect x="6" y="6" width="36" height="10" rx="2"/><rect x="10" y="16" width="28" height="26" rx="2"/><path d="M20 26 H28"/>`,
  publish: c`<path d="M24 6 V28 M24 6 L16 14 M24 6 L32 14"/><path d="M8 32 V38 A2 2 0 0 0 10 40 H38 A2 2 0 0 0 40 38 V32"/>`,
  changelog: c`<path d="M10 6 H28 L36 14 V42 H10 Z"/><path d="M28 6 V14 H36"/><path d="M16 24 H24"/><circle cx="34" cy="34" r="7"/><path d="M34 30 V34 L37 36"/>`,
  vulnerability: c`<path d="M24 6 L40 12 V22 C40 33 33 40 24 43 C15 40 8 33 8 22 V12 Z"/><path d="M24 16 V26"/><circle cx="24" cy="32" r="1.8" fill="currentColor" stroke="none"/>`,
  gauge: c`<path d="M6 34 A18 18 0 0 1 42 34"/><path d="M24 34 L34 20"/><circle cx="24" cy="34" r="2.2" fill="currentColor" stroke="none"/>`,
  "file-tree": c`<path d="M6 12 H18 L22 17 H42 V38 H6 Z"/><path d="M26 24 H36 M26 30 H36"/>`,
  "check-circle": c`<circle cx="24" cy="24" r="17"/><path d="M15 24 L21 30 L33 17"/>`,
  "x-circle": c`<circle cx="24" cy="24" r="17"/><path d="M18 18 L30 30 M30 18 L18 30"/>`,
  fork: c`<circle cx="14" cy="10" r="4"/><circle cx="34" cy="10" r="4"/><circle cx="24" cy="38" r="4"/><path d="M14 14 V22 L24 32 M34 14 V22 L24 32 M24 32 V34"/>`,
  trending: c`<path d="M6 34 L18 22 L26 28 L42 10"/><path d="M32 10 H42 V20"/>`,
  collection: c`<rect x="10" y="6" width="28" height="10" rx="2"/><rect x="6" y="19" width="36" height="10" rx="2"/><rect x="10" y="32" width="28" height="10" rx="2"/>`,
  // Phase 4: fleet console (Puppet Enterprise replacement) ---------------
  dashboard: c`<rect x="6" y="6" width="18" height="14" rx="2"/><rect x="28" y="6" width="14" height="8" rx="2"/><rect x="28" y="18" width="14" height="14" rx="2"/><rect x="6" y="24" width="18" height="18" rx="2"/>`,
  node: c`<rect x="10" y="14" width="28" height="20" rx="3"/><path d="M16 22 H22 M16 27 H22"/><circle cx="32" cy="24" r="2" fill="currentColor" stroke="none"/>`,
  "node-group": c`<rect x="6" y="10" width="24" height="9" rx="2"/><rect x="12" y="21" width="24" height="9" rx="2"/><rect x="18" y="32" width="24" height="9" rx="2"/>`,
  pulse: c`<path d="M4 24 H14 L19 12 L27 36 L32 24 H44"/>`,
  compliance: c`<rect x="9" y="6" width="30" height="36" rx="3"/><path d="M16 16 L19 19 L26 12 M16 26 L19 29 L26 22 M16 36 H30"/>`,
  report: c`<path d="M10 6 H28 L36 14 V42 H10 Z"/><path d="M28 6 V14 H36"/><path d="M16 34 V28 M22 34 V24 M28 34 V30"/>`,
  "activity-log": c`<path d="M14 8 V40"/><circle cx="14" cy="12" r="3" fill="currentColor" stroke="none"/><circle cx="14" cy="24" r="3" fill="currentColor" stroke="none"/><circle cx="14" cy="36" r="3" fill="currentColor" stroke="none"/><path d="M22 12 H40 M22 24 H40 M22 36 H34"/>`,
  bell: c`<path d="M12 32 V22 A12 12 0 0 1 36 22 V32 L40 38 H8 Z"/><path d="M20 38 A4 4 0 0 0 28 38"/>`,
  orchestrate: c`<circle cx="24" cy="24" r="17"/><path d="M19 15 L33 24 L19 33 Z"/>`,
  plan: c`<rect x="6" y="8" width="24" height="32" rx="3"/><path d="M12 16 H24 M12 22 H24 M12 28 H20"/><path d="M32 24 H44 M38 18 L44 24 L38 30"/>`,
  badge: c`<rect x="6" y="10" width="36" height="28" rx="3"/><circle cx="17" cy="21" r="5"/><path d="M10 32 C10 26 13 24 17 24 C21 24 24 26 24 32"/><path d="M30 18 H38 M30 24 H38 M30 30 H36"/>`,
  audit: c`<path d="M10 6 H26 L34 14 V42 H10 Z"/><path d="M26 6 V14 H34"/><path d="M16 24 H24 M16 30 H22"/><circle cx="33" cy="33" r="6"/><path d="M37.5 37.5 L43 43"/>`,
  key: c`<circle cx="16" cy="24" r="9"/><path d="M23 24 H42 M34 24 V31 M40 24 V29"/>`,
  layers: c`<path d="M24 6 L44 16 L24 26 L4 16 Z"/><path d="M4 26 L24 36 L44 26"/><path d="M4 34 L24 44 L44 34"/>`,
  classifier: c`<path d="M6 10 H24 L40 26 L24 42 H6 Z"/><circle cx="14" cy="18" r="3" fill="currentColor" stroke="none"/><circle cx="32" cy="26" r="3" fill="currentColor" stroke="none"/>`,
  deploy: c`<path d="M24 8 V34"/><path d="M24 8 L15 20 M24 8 L33 20"/><path d="M10 40 H38"/>`,
  metrics: c`<path d="M6 40 H42"/><path d="M6 40 V6"/><path d="M10 30 L18 22 L26 27 L38 12"/>`,
  schedule: c`<rect x="6" y="10" width="28" height="30" rx="3"/><path d="M6 18 H34"/><path d="M14 6 V14 M26 6 V14"/><circle cx="34" cy="34" r="10"/><path d="M34 28 V34 L38 37"/>`,
  backup: c`<path d="M24 6 V26 M24 26 L16 18 M24 26 L32 18"/><rect x="8" y="30" width="32" height="12" rx="2"/><path d="M8 36 H40"/>`,
  webhook: c`<circle cx="12" cy="14" r="6"/><circle cx="36" cy="34" r="6"/><path d="M17 17 L31 31"/><path d="M22 22 L20 28 L26 26 L24 32"/>`,
  organization: c`<rect x="10" y="10" width="28" height="32" rx="2"/><path d="M17 18 H21 M27 18 H31 M17 26 H21 M27 26 H31 M17 34 H21 M27 34 H31"/>`,
  drift: c`<rect x="6" y="12" width="20" height="20" rx="3"/><rect x="22" y="16" width="20" height="20" rx="3"/><path d="M20 24 H28"/>`,
  // Added later: consolidated from docs pages that predated vox-icon ------
  blocks: c`<rect x="7" y="7" width="15" height="15" rx="2.5"/><rect x="26" y="7" width="15" height="15" rx="2.5"/><rect x="7" y="26" width="15" height="15" rx="2.5"/><rect x="26" y="26" width="15" height="15" rx="2.5"/>`,
  plug: c`<path d="M16 6 V18 M32 6 V18 M12 18 H36 V28 C36 34 30 38 24 38 C18 38 12 34 12 28 Z"/><path d="M24 38 V44"/>`,
  ballot: c`<rect x="8" y="18" width="32" height="24" rx="2.5"/><path d="M8 26 H40"/><path d="M24 10 V26"/><path d="M18 6 L24 12 L30 6"/>`,
  sun: c`<circle cx="24" cy="24" r="8"/><path d="M24 4v4M24 40v4M9.86 9.86l2.82 2.82M35.32 35.32l2.82 2.82M4 24h4M40 24h4M12.68 35.32l-2.82 2.82M38.14 9.86l-2.82 2.82"/>`,
  moon: c`<path d="M42 25.58A18 18 0 1 1 22.42 6 14 14 0 0 0 42 25.58Z"/>`,
  github: c`<g transform="translate(4,4) scale(2.5)" fill="currentColor" stroke="none"><path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0 0 16 8c0-4.42-3.58-8-8-8z"/></g>`,
  accessibility: c`<circle cx="24" cy="24" r="18"/><circle cx="24" cy="16" r="2.6" fill="currentColor" stroke="none"/><path d="M14 21 H34 M24 21 V32 M24 25 L18 34 M24 25 L30 34"/>`,
  settings: c`<circle cx="24" cy="24" r="7"/><path d="M24 6 V12 M24 36 V42 M6 24 H12 M36 24 H42 M11 11 L15.2 15.2 M32.8 32.8 L37 37 M37 11 L32.8 15.2 M15.2 32.8 L11 37"/>`
};
var er = Object.defineProperty, tr = Object.getOwnPropertyDescriptor, ct = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? tr(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && er(e, o, t), t;
};
let W = class extends p {
  constructor() {
    super(...arguments), this.name = "info", this.size = "md";
  }
  render() {
    const r = V[this.name];
    return r ? l`
      <svg
        viewBox="0 0 48 48"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
        stroke-linecap="round"
        stroke-linejoin="round"
        role=${this.label ? "img" : x}
        aria-hidden=${this.label ? x : "true"}
        aria-label=${this.label ?? x}
      >
        ${r}
      </svg>
    ` : x;
  }
};
W.styles = d`
    :host {
      display: inline-flex;
      flex: none;
      color: inherit;
    }

    svg {
      display: block;
    }

    :host([size='sm']) svg {
      width: 16px;
      height: 16px;
    }

    :host([size='md']) svg {
      width: 20px;
      height: 20px;
    }

    :host([size='lg']) svg {
      width: 24px;
      height: 24px;
    }

    :host([size='xl']) svg {
      width: 48px;
      height: 48px;
    }
  `;
ct([
  n()
], W.prototype, "name", 2);
ct([
  n({ reflect: !0 })
], W.prototype, "size", 2);
ct([
  n()
], W.prototype, "label", 2);
W = ct([
  h("vox-icon")
], W);
var or = Object.defineProperty, je = (r, e, o, s) => {
  for (var t = void 0, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = i(e, o, t) || t);
  return t && or(e, o, t), t;
};
const Wt = class Wt extends p {
  constructor() {
    super(), this.name = "", this.label = "", this.note = "", this.disabled = !1, this.required = !1, this.internals = this.attachInternals();
  }
  get form() {
    return this.internals.form;
  }
  get validity() {
    return this.internals.validity;
  }
  get validationMessage() {
    return this.internals.validationMessage;
  }
  checkValidity() {
    return this.internals.checkValidity();
  }
  reportValidity() {
    return this.internals.reportValidity();
  }
  /** Mirror a native inner control's validity onto the host element. */
  syncValidity(e) {
    this.internals.setValidity(e.validity, e.validationMessage, e);
  }
  renderLabel(e) {
    return this.label ? l`
      <label class="label" for=${e}>
        ${this.label}${this.required ? l`<span class="required-mark" aria-hidden="true"> *</span>` : x}
      </label>
    ` : x;
  }
  renderNote() {
    return this.note ? l`<p class="note">${this.note}</p>` : x;
  }
};
Wt.formAssociated = !0;
let m = Wt;
je([
  n()
], m.prototype, "name");
je([
  n()
], m.prototype, "label");
je([
  n()
], m.prototype, "note");
je([
  n({ type: Boolean, reflect: !0 })
], m.prototype, "disabled");
je([
  n({ type: Boolean, reflect: !0 })
], m.prototype, "required");
const Ve = d`
  :host {
    display: block;
    font-family: var(--vox-font-family-base);
  }

  :host([disabled]) {
    opacity: 0.6;
    pointer-events: none;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: var(--vox-space-1);
  }

  .label {
    font-size: 14px;
    font-weight: 600;
    color: var(--vox-color-text-1);
  }

  .required-mark {
    color: var(--vox-color-danger-1);
  }

  .note {
    margin: 0;
    font-size: 12px;
    color: var(--vox-color-text-2);
  }

  .control {
    box-sizing: border-box;
    width: 100%;
    background-color: var(--vox-color-bg);
    border: 1px solid var(--vox-color-border);
    border-radius: var(--vox-radius-md);
    color: var(--vox-color-text-1);
    font-family: inherit;
    font-size: 14px;
    line-height: 1.5;
    padding: var(--vox-space-2) var(--vox-space-3);
    transition:
      border-color var(--vox-transition-fast),
      box-shadow var(--vox-transition-fast);
  }

  .control:focus {
    outline: none;
    border-color: var(--vox-color-brand-1);
    box-shadow: 0 0 0 3px var(--vox-color-brand-soft);
  }

  /* Corner flattening when placed inside a <vox-input-group>. */
  :host([data-vox-group]) .control {
    border-radius: 0;
  }

  :host([data-vox-group='start']) .control {
    border-radius: var(--vox-radius-md) 0 0 var(--vox-radius-md);
  }

  :host([data-vox-group='end']) .control {
    border-radius: 0 var(--vox-radius-md) var(--vox-radius-md) 0;
  }
`, go = d`
  :host {
    display: block;
    font-family: var(--vox-font-family-base);
  }

  :host([disabled]) {
    opacity: 0.6;
    pointer-events: none;
  }

  .check {
    display: inline-flex;
    align-items: flex-start;
    gap: var(--vox-space-2);
    cursor: pointer;
    font-size: 14px;
    line-height: 1.5;
    color: var(--vox-color-text-1);
  }

  input {
    position: absolute;
    width: 1px;
    height: 1px;
    opacity: 0;
    margin: 0;
  }
`;
var rr = Object.defineProperty, sr = Object.getOwnPropertyDescriptor, Dt = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? sr(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && rr(e, o, t), t;
};
let pe = class extends m {
  constructor() {
    super(...arguments), this.checked = !1, this.value = "on";
  }
  formResetCallback() {
    this.checked = !1;
  }
  updated() {
    this.internals.setFormValue(this.checked ? this.value : null), this.internals.setValidity(
      this.required && !this.checked ? { valueMissing: !0 } : {},
      "Please check this box.",
      this.renderRoot.querySelector("input") ?? void 0
    );
  }
  handleChange(r) {
    this.checked = r.target.checked, this.dispatchEvent(new Event("change", { bubbles: !0 }));
  }
  render() {
    return l`
      <label class="check">
        <input
          type="checkbox"
          .checked=${this.checked}
          ?disabled=${this.disabled}
          @change=${this.handleChange}
        />
        <span class="box" aria-hidden="true">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="4" stroke-linecap="round" stroke-linejoin="round">
            <path d="m4 12 5 5L20 6" />
          </svg>
        </span>
        <span class="text"><slot></slot></span>
      </label>
    `;
  }
};
pe.styles = [
  go,
  d`
      .box {
        flex: none;
        display: flex;
        align-items: center;
        justify-content: center;
        width: 18px;
        height: 18px;
        margin-top: 2px;
        border: 1px solid var(--vox-color-border);
        border-radius: var(--vox-radius-sm);
        background-color: var(--vox-color-bg);
        color: var(--vox-color-text-inverse);
        transition:
          background-color var(--vox-transition-fast),
          border-color var(--vox-transition-fast);
      }

      .box svg {
        width: 12px;
        height: 12px;
        opacity: 0;
      }

      input:checked + .box {
        background-color: var(--vox-color-brand-3);
        border-color: var(--vox-color-brand-3);
      }

      input:checked + .box svg {
        opacity: 1;
      }

      input:focus-visible + .box {
        outline: 2px solid var(--vox-color-brand-1);
        outline-offset: 2px;
      }
    `
];
Dt([
  n({ type: Boolean, reflect: !0 })
], pe.prototype, "checked", 2);
Dt([
  n()
], pe.prototype, "value", 2);
pe = Dt([
  h("vox-checkbox")
], pe);
var ar = Object.defineProperty, ir = Object.getOwnPropertyDescriptor, re = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? ir(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && ar(e, o, t), t;
};
let P = class extends m {
  constructor() {
    super(...arguments), this.multiple = !1, this.buttonLabel = "Choose a file", this.fileNames = [];
  }
  formResetCallback() {
    this.fileNames = [], this.inputEl && (this.inputEl.value = ""), this.internals.setFormValue(null);
  }
  handleChange() {
    const r = [...this.inputEl.files ?? []];
    this.fileNames = r.map((o) => o.name);
    const e = new FormData();
    for (const o of r) e.append(this.name, o);
    this.internals.setFormValue(r.length > 0 ? e : null), this.internals.setValidity(
      this.required && r.length === 0 ? { valueMissing: !0 } : {},
      "Please select a file.",
      this.inputEl
    ), this.dispatchEvent(new Event("change", { bubbles: !0 }));
  }
  render() {
    return l`
      <div class="field">
        ${this.renderLabel("file")}
        <label>
          <input
            id="file"
            type="file"
            accept=${Z(this.accept)}
            ?multiple=${this.multiple}
            ?disabled=${this.disabled}
            @change=${this.handleChange}
          />
          <span class="picker">
            <span class="button">${this.buttonLabel}</span>
            <span class="names">
              ${this.fileNames.length > 0 ? this.fileNames.join(", ") : "No file selected"}
            </span>
          </span>
        </label>
        ${this.renderNote()}
      </div>
    `;
  }
};
P.styles = [
  Ve,
  d`
      input {
        position: absolute;
        width: 1px;
        height: 1px;
        opacity: 0;
      }

      .picker {
        display: flex;
        align-items: center;
        gap: var(--vox-space-3);
        flex-wrap: wrap;
      }

      .button {
        display: inline-flex;
        align-items: center;
        padding: 0 var(--vox-space-4);
        height: 34px;
        background-color: var(--vox-color-bg-soft);
        border: 1px dashed var(--vox-color-border);
        border-radius: var(--vox-radius-md);
        color: var(--vox-color-text-1);
        font-size: 14px;
        font-weight: 600;
        cursor: pointer;
        transition:
          border-color var(--vox-transition-fast),
          color var(--vox-transition-fast);
      }

      .button:hover {
        border-color: var(--vox-color-brand-1);
        color: var(--vox-color-brand-1);
      }

      input:focus-visible ~ .picker .button {
        outline: 2px solid var(--vox-color-brand-1);
        outline-offset: 2px;
      }

      .names {
        font-size: 13px;
        color: var(--vox-color-text-2);
      }
    `
];
re([
  n()
], P.prototype, "accept", 2);
re([
  n({ type: Boolean })
], P.prototype, "multiple", 2);
re([
  n({ attribute: "button-label" })
], P.prototype, "buttonLabel", 2);
re([
  nt()
], P.prototype, "fileNames", 2);
re([
  jt("input")
], P.prototype, "inputEl", 2);
P = re([
  h("vox-file-input")
], P);
/**
 * @license
 * Copyright 2020 Google LLC
 * SPDX-License-Identifier: BSD-3-Clause
 */
const nr = (r) => r.strings === void 0, lr = {}, cr = (r, e = lr) => r._$AH = e;
/**
 * @license
 * Copyright 2020 Google LLC
 * SPDX-License-Identifier: BSD-3-Clause
 */
const bo = xo(class extends fo {
  constructor(r) {
    if (super(r), r.type !== L.PROPERTY && r.type !== L.ATTRIBUTE && r.type !== L.BOOLEAN_ATTRIBUTE) throw Error("The `live` directive is not allowed on child or event bindings");
    if (!nr(r)) throw Error("`live` bindings can only contain a single expression");
  }
  render(r) {
    return r;
  }
  update(r, [e]) {
    if (e === y || e === x) return e;
    const o = r.element, s = r.name;
    if (r.type === L.PROPERTY) {
      if (e === o[s]) return y;
    } else if (r.type === L.BOOLEAN_ATTRIBUTE) {
      if (!!e === o.hasAttribute(s)) return y;
    } else if (r.type === L.ATTRIBUTE && o.getAttribute(s) === e + "") return y;
    return cr(r), e;
  }
});
var dr = Object.defineProperty, hr = Object.getOwnPropertyDescriptor, se = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? hr(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && dr(e, o, t), t;
};
let A = class extends m {
  constructor() {
    super(...arguments), this.type = "text", this.value = "", this.readonly = !1;
  }
  formResetCallback() {
    this.value = "";
  }
  updated() {
    this.internals.setFormValue(this.value);
    const r = this.renderRoot.querySelector("input");
    r && this.syncValidity(r);
  }
  focus(r) {
    var e;
    (e = this.renderRoot.querySelector("input")) == null || e.focus(r);
  }
  handleInput(r) {
    this.value = r.target.value;
  }
  handleChange() {
    this.dispatchEvent(new Event("change", { bubbles: !0 }));
  }
  render() {
    return l`
      <div class="field">
        ${this.renderLabel("input")}
        <input
          id="input"
          class="control"
          type=${this.type}
          .value=${bo(this.value)}
          placeholder=${Z(this.placeholder)}
          autocomplete=${Z(this.autocomplete)}
          ?required=${this.required}
          ?readonly=${this.readonly}
          ?disabled=${this.disabled}
          aria-label=${this.label ? x : "text input"}
          @input=${this.handleInput}
          @change=${this.handleChange}
        />
        ${this.renderNote()}
      </div>
    `;
  }
};
A.styles = Ve;
se([
  n()
], A.prototype, "type", 2);
se([
  n()
], A.prototype, "value", 2);
se([
  n()
], A.prototype, "placeholder", 2);
se([
  n()
], A.prototype, "autocomplete", 2);
se([
  n({ type: Boolean, reflect: !0 })
], A.prototype, "readonly", 2);
A = se([
  h("vox-input")
], A);
var pr = Object.getOwnPropertyDescriptor, vr = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? pr(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = i(t) || t);
  return t;
};
let $t = class extends p {
  handleSlotChange(r) {
    const e = r.target.assignedElements();
    e.forEach((o, s) => {
      const t = s === 0 ? "start" : s === e.length - 1 ? "end" : "middle";
      o.setAttribute("data-vox-group", t);
    });
  }
  render() {
    return l`
      <div class="group" role="group">
        <slot @slotchange=${this.handleSlotChange}></slot>
      </div>
    `;
  }
};
$t.styles = d`
    :host {
      display: block;
      font-family: var(--vox-font-family-base);
    }

    .group {
      display: flex;
      align-items: stretch;
    }

    ::slotted(*) {
      flex: none;
    }

    ::slotted(vox-input),
    ::slotted(vox-select) {
      flex: 1 1 auto;
      min-width: 0;
    }

    ::slotted(span) {
      display: inline-flex;
      align-items: center;
      padding: 0 var(--vox-space-3);
      background-color: var(--vox-color-bg-soft);
      border: 1px solid var(--vox-color-border);
      color: var(--vox-color-text-2);
      font-size: 14px;
      white-space: nowrap;
    }

    ::slotted(span[data-vox-group='start']) {
      border-radius: var(--vox-radius-md) 0 0 var(--vox-radius-md);
      border-right: none;
    }

    ::slotted(span[data-vox-group='end']) {
      border-radius: 0 var(--vox-radius-md) var(--vox-radius-md) 0;
      border-left: none;
    }
  `;
$t = vr([
  h("vox-input-group")
], $t);
var ur = Object.defineProperty, xr = Object.getOwnPropertyDescriptor, dt = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? xr(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && ur(e, o, t), t;
};
let G = class extends p {
  constructor() {
    super(...arguments), this.value = "", this.checked = !1, this.disabled = !1;
  }
  select() {
    this.disabled || this.dispatchEvent(
      new CustomEvent("vox-radio-select", { bubbles: !0, composed: !0 })
    );
  }
  handleKeydown(r) {
    (r.key === " " || r.key === "Enter") && (r.preventDefault(), this.select());
  }
  render() {
    return l`
      <span
        class="radio"
        role="radio"
        aria-checked=${this.checked ? "true" : "false"}
        aria-disabled=${this.disabled ? "true" : "false"}
        tabindex=${this.checked ? "0" : "-1"}
        @click=${this.select}
        @keydown=${this.handleKeydown}
      >
        <span class="circle" aria-hidden="true"></span>
        <span class="text"><slot></slot></span>
      </span>
    `;
  }
};
G.styles = d`
    :host {
      display: block;
      font-family: var(--vox-font-family-base);
    }

    :host([disabled]) {
      opacity: 0.6;
      pointer-events: none;
    }

    .radio {
      display: inline-flex;
      align-items: flex-start;
      gap: var(--vox-space-2);
      cursor: pointer;
      font-size: 14px;
      line-height: 1.5;
      color: var(--vox-color-text-1);
    }

    .radio:focus {
      outline: none;
    }

    .circle {
      flex: none;
      box-sizing: border-box;
      width: 18px;
      height: 18px;
      margin-top: 2px;
      border: 1px solid var(--vox-color-border);
      border-radius: 50%;
      background-color: var(--vox-color-bg);
      transition:
        border-color var(--vox-transition-fast),
        box-shadow var(--vox-transition-fast);
    }

    :host([checked]) .circle {
      border-color: var(--vox-color-brand-3);
      border-width: 5px;
    }

    .radio:focus-visible .circle {
      outline: 2px solid var(--vox-color-brand-1);
      outline-offset: 2px;
    }
  `;
dt([
  n()
], G.prototype, "value", 2);
dt([
  n({ type: Boolean, reflect: !0 })
], G.prototype, "checked", 2);
dt([
  n({ type: Boolean, reflect: !0 })
], G.prototype, "disabled", 2);
G = dt([
  h("vox-radio")
], G);
var fr = Object.defineProperty, gr = Object.getOwnPropertyDescriptor, mo = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? gr(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && fr(e, o, t), t;
};
let Ge = class extends m {
  constructor() {
    super(...arguments), this.value = "";
  }
  get radios() {
    return [...this.querySelectorAll("vox-radio")];
  }
  formResetCallback() {
    this.value = "";
  }
  updated() {
    this.internals.setFormValue(this.value || null), this.internals.setValidity(
      this.required && !this.value ? { valueMissing: !0 } : {},
      "Please select an option.",
      this
    ), this.syncRadios();
  }
  syncRadios() {
    const r = this.radios, e = r.some((o) => o.value === this.value && this.value !== "");
    r.forEach((o, s) => {
      var a, i;
      o.checked = this.value !== "" && o.value === this.value;
      const t = e ? o.checked : s === 0;
      (i = (a = o.shadowRoot) == null ? void 0 : a.querySelector(".radio")) == null || i.setAttribute("tabindex", t ? "0" : "-1");
    });
  }
  handleSelect(r) {
    const e = r.target;
    !(e instanceof HTMLElement) || e.tagName !== "VOX-RADIO" || (r.stopPropagation(), this.value !== e.value && (this.value = e.value, this.dispatchEvent(new Event("change", { bubbles: !0 }))));
  }
  handleKeydown(r) {
    var u, v;
    const o = {
      ArrowDown: 1,
      ArrowRight: 1,
      ArrowUp: -1,
      ArrowLeft: -1
    }[r.key];
    if (!o) return;
    r.preventDefault();
    const s = this.radios.filter((f) => !f.disabled);
    if (s.length === 0) return;
    const t = s.findIndex((f) => f.checked), a = (Math.max(t, 0) + o + s.length) % s.length, i = s[a];
    i.select(), (v = (u = i.shadowRoot) == null ? void 0 : u.querySelector(".radio")) == null || v.focus();
  }
  render() {
    return l`
      <div class="field" role="radiogroup" aria-label=${this.label}>
        ${this.label ? l`<span class="label">
              ${this.label}${this.required ? l`<span class="required-mark" aria-hidden="true"> *</span>` : ""}
            </span>` : ""}
        <div
          class="options"
          @vox-radio-select=${this.handleSelect}
          @keydown=${this.handleKeydown}
        >
          <slot @slotchange=${this.syncRadios}></slot>
        </div>
        ${this.renderNote()}
      </div>
    `;
  }
};
Ge.styles = [
  Ve,
  d`
      .options {
        display: flex;
        flex-direction: column;
        gap: var(--vox-space-2);
        margin-top: var(--vox-space-1);
      }
    `
];
mo([
  n()
], Ge.prototype, "value", 2);
Ge = mo([
  h("vox-radio-group")
], Ge);
var br = Object.defineProperty, mr = Object.getOwnPropertyDescriptor, zt = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? mr(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && br(e, o, t), t;
};
let ve = class extends m {
  constructor() {
    super(...arguments), this.value = "";
  }
  formResetCallback() {
    this.value = "", this.syncOptions();
  }
  updated() {
    this.internals.setFormValue(this.value), this.selectEl && this.syncValidity(this.selectEl);
  }
  syncOptions() {
    if (!this.selectEl) return;
    const r = this.renderRoot.querySelector("slot");
    r && (this.selectEl.replaceChildren(
      ...r.assignedElements().filter((e) => e instanceof HTMLOptionElement || e instanceof HTMLOptGroupElement).map((e) => e.cloneNode(!0))
    ), this.value && (this.selectEl.value = this.value), this.value = this.selectEl.value);
  }
  handleChange() {
    this.value = this.selectEl.value, this.dispatchEvent(new Event("change", { bubbles: !0 }));
  }
  render() {
    return l`
      <div class="field">
        ${this.renderLabel("select")}
        <select
          id="select"
          class="control"
          ?required=${this.required}
          ?disabled=${this.disabled}
          @change=${this.handleChange}
        ></select>
        ${this.renderNote()}
      </div>
      <div hidden><slot @slotchange=${this.syncOptions}></slot></div>
    `;
  }
};
ve.styles = [
  Ve,
  d`
      select.control {
        appearance: none;
        padding-right: var(--vox-space-8);
        background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='16' height='16' viewBox='0 0 24 24' fill='none' stroke='%23808080' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'%3E%3Cpath d='m6 9 6 6 6-6'/%3E%3C/svg%3E");
        background-repeat: no-repeat;
        background-position: right var(--vox-space-3) center;
        cursor: pointer;
      }
    `
];
zt([
  n()
], ve.prototype, "value", 2);
zt([
  jt("select")
], ve.prototype, "selectEl", 2);
ve = zt([
  h("vox-select")
], ve);
var yr = Object.defineProperty, $r = Object.getOwnPropertyDescriptor, Ht = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? $r(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && yr(e, o, t), t;
};
let ue = class extends m {
  constructor() {
    super(...arguments), this.checked = !1, this.value = "on";
  }
  formResetCallback() {
    this.checked = !1;
  }
  updated() {
    this.internals.setFormValue(this.checked ? this.value : null);
  }
  handleChange(r) {
    this.checked = r.target.checked, this.dispatchEvent(new Event("change", { bubbles: !0 }));
  }
  render() {
    return l`
      <label class="check">
        <input
          type="checkbox"
          role="switch"
          .checked=${this.checked}
          ?disabled=${this.disabled}
          @change=${this.handleChange}
        />
        <span class="track" aria-hidden="true"></span>
        <span class="text"><slot></slot></span>
      </label>
    `;
  }
};
ue.styles = [
  go,
  d`
      .track {
        flex: none;
        position: relative;
        width: 36px;
        height: 20px;
        margin-top: 1px;
        border-radius: var(--vox-radius-full);
        background-color: var(--vox-color-border);
        transition: background-color var(--vox-transition-fast);
      }

      .track::after {
        content: '';
        position: absolute;
        top: 2px;
        left: 2px;
        width: 16px;
        height: 16px;
        border-radius: 50%;
        background-color: var(--vox-color-bg);
        transition: transform var(--vox-transition-fast);
      }

      input:checked + .track {
        background-color: var(--vox-color-brand-3);
      }

      input:checked + .track::after {
        transform: translateX(16px);
      }

      input:focus-visible + .track {
        outline: 2px solid var(--vox-color-brand-1);
        outline-offset: 2px;
      }
    `
];
Ht([
  n({ type: Boolean, reflect: !0 })
], ue.prototype, "checked", 2);
Ht([
  n()
], ue.prototype, "value", 2);
ue = Ht([
  h("vox-switch")
], ue);
var wr = Object.defineProperty, _r = Object.getOwnPropertyDescriptor, De = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? _r(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && wr(e, o, t), t;
};
let D = class extends m {
  constructor() {
    super(...arguments), this.value = "", this.rows = 4, this.readonly = !1;
  }
  formResetCallback() {
    this.value = "";
  }
  updated() {
    this.internals.setFormValue(this.value);
    const r = this.renderRoot.querySelector("textarea");
    r && this.syncValidity(r);
  }
  focus(r) {
    var e;
    (e = this.renderRoot.querySelector("textarea")) == null || e.focus(r);
  }
  handleInput(r) {
    this.value = r.target.value;
  }
  handleChange() {
    this.dispatchEvent(new Event("change", { bubbles: !0 }));
  }
  render() {
    return l`
      <div class="field">
        ${this.renderLabel("textarea")}
        <textarea
          id="textarea"
          class="control"
          rows=${this.rows}
          .value=${bo(this.value)}
          placeholder=${Z(this.placeholder)}
          ?required=${this.required}
          ?readonly=${this.readonly}
          ?disabled=${this.disabled}
          @input=${this.handleInput}
          @change=${this.handleChange}
        ></textarea>
        ${this.renderNote()}
      </div>
    `;
  }
};
D.styles = [
  Ve,
  d`
      textarea.control {
        resize: vertical;
        min-height: 4em;
      }
    `
];
De([
  n()
], D.prototype, "value", 2);
De([
  n()
], D.prototype, "placeholder", 2);
De([
  n({ type: Number })
], D.prototype, "rows", 2);
De([
  n({ type: Boolean, reflect: !0 })
], D.prototype, "readonly", 2);
D = De([
  h("vox-textarea")
], D);
var Cr = Object.defineProperty, Or = Object.getOwnPropertyDescriptor, ze = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? Or(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && Cr(e, o, t), t;
};
let z = class extends p {
  constructor() {
    super(...arguments), this.alt = "", this.initials = "", this.size = "md";
  }
  render() {
    return l`
      <span class="avatar" role=${this.src ? "presentation" : "img"} aria-label=${this.alt}>
        ${this.src ? l`<img src=${this.src} alt=${this.alt} />` : this.initials}
      </span>
    `;
  }
};
z.styles = d`
    :host {
      display: inline-block;
    }

    .avatar {
      display: flex;
      align-items: center;
      justify-content: center;
      overflow: hidden;
      border-radius: 50%;
      background-color: var(--vox-color-brand-soft);
      color: var(--vox-color-brand-1);
      font-family: var(--vox-font-family-base);
      font-weight: 600;
      user-select: none;
    }

    img {
      width: 100%;
      height: 100%;
      object-fit: cover;
    }

    :host([size='sm']) .avatar {
      width: 28px;
      height: 28px;
      font-size: 11px;
    }

    :host([size='md']) .avatar {
      width: 40px;
      height: 40px;
      font-size: 14px;
    }

    :host([size='lg']) .avatar {
      width: 56px;
      height: 56px;
      font-size: 20px;
    }

    :host([size='xl']) .avatar {
      width: 80px;
      height: 80px;
      font-size: 28px;
    }
  `;
ze([
  n()
], z.prototype, "src", 2);
ze([
  n()
], z.prototype, "alt", 2);
ze([
  n()
], z.prototype, "initials", 2);
ze([
  n({ reflect: !0 })
], z.prototype, "size", 2);
z = ze([
  h("vox-avatar")
], z);
var Mr = Object.defineProperty, Pr = Object.getOwnPropertyDescriptor, Tt = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? Pr(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && Mr(e, o, t), t;
};
let xe = class extends p {
  constructor() {
    super(...arguments), this.heading = "", this.reverse = !1, this.hasActions = !1;
  }
  handleActionsSlotChange(r) {
    const e = r.target;
    this.hasActions = e.assignedNodes({ flatten: !0 }).length > 0, this.requestUpdate();
  }
  render() {
    return l`
      <div class="billboard">
        <div class="media"><slot name="media"></slot></div>
        <div class="content">
          <h2 class="heading">${this.heading}</h2>
          <div class="body"><slot></slot></div>
          <div class="actions ${this.hasActions ? "has-content" : ""}">
            <slot name="actions" @slotchange=${this.handleActionsSlotChange}></slot>
          </div>
        </div>
      </div>
    `;
  }
};
xe.styles = d`
    :host {
      display: block;
      font-family: var(--vox-font-family-base);
    }

    .billboard {
      display: flex;
      gap: var(--vox-space-8);
      align-items: center;
      flex-wrap: wrap;
      padding: var(--vox-space-8);
      background-color: var(--vox-color-bg-soft);
      border-radius: var(--vox-radius-lg);
    }

    :host([reverse]) .billboard {
      flex-direction: row-reverse;
    }

    .media {
      flex: 1 1 280px;
      min-width: 0;
    }

    .media ::slotted(img) {
      display: block;
      max-width: 100%;
      border-radius: var(--vox-radius-md);
    }

    .content {
      flex: 1 1 320px;
      min-width: 0;
    }

    .heading {
      margin: 0 0 var(--vox-space-3);
      font-family: var(--vox-font-family-display);
      font-size: 28px;
      font-weight: 600;
      line-height: 1.3;
      color: var(--vox-color-text-1);
    }

    .body {
      font-size: 16px;
      line-height: 1.7;
      color: var(--vox-color-text-2);
    }

    .actions {
      display: flex;
      gap: var(--vox-space-3);
      flex-wrap: wrap;
      margin-top: var(--vox-space-6);
    }

    .actions:not(.has-content) {
      display: none;
    }
  `;
Tt([
  n()
], xe.prototype, "heading", 2);
Tt([
  n({ type: Boolean, reflect: !0 })
], xe.prototype, "reverse", 2);
xe = Tt([
  h("vox-billboard")
], xe);
var Ar = Object.getOwnPropertyDescriptor, kr = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? Ar(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = i(t) || t);
  return t;
};
let wt = class extends p {
  render() {
    return l`
      <nav aria-label="Breadcrumbs">
        <slot></slot>
      </nav>
    `;
  }
};
wt.styles = d`
    :host {
      display: block;
      font-family: var(--vox-font-family-base);
    }

    nav {
      display: flex;
      align-items: center;
      flex-wrap: wrap;
      font-size: 13px;
    }

    ::slotted(*) {
      color: var(--vox-color-text-2);
      text-decoration: none;
    }

    ::slotted(a:hover) {
      color: var(--vox-color-brand-1);
      text-decoration: underline;
    }

    ::slotted([aria-current='page']) {
      color: var(--vox-color-text-1);
      font-weight: 600;
    }

    ::slotted(*:not(:first-child))::before {
      content: '/';
      margin: 0 var(--vox-space-2);
      color: var(--vox-color-text-3);
    }
  `;
wt = kr([
  h("vox-breadcrumbs")
], wt);
var Lr = Object.defineProperty, Er = Object.getOwnPropertyDescriptor, ht = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? Er(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && Lr(e, o, t), t;
};
let X = class extends p {
  constructor() {
    super(...arguments), this.siteTitle = "", this.href = "/", this.mobileOpen = !1, this.handleOutsideClick = (r) => {
      this.mobileOpen && !r.composedPath().includes(this) && (this.mobileOpen = !1);
    }, this.handleKeydown = (r) => {
      var e;
      r.key === "Escape" && this.mobileOpen && (this.mobileOpen = !1, (e = this.renderRoot.querySelector(".menu-toggle")) == null || e.focus());
    }, this.toggleMobileMenu = () => {
      this.mobileOpen = !this.mobileOpen;
    }, this.closeMobileMenu = () => {
      this.mobileOpen = !1;
    };
  }
  connectedCallback() {
    super.connectedCallback(), document.addEventListener("click", this.handleOutsideClick), this.addEventListener("keydown", this.handleKeydown);
  }
  disconnectedCallback() {
    super.disconnectedCallback(), document.removeEventListener("click", this.handleOutsideClick), this.removeEventListener("keydown", this.handleKeydown);
  }
  render() {
    return l`
      <header class="header">
        <a class="brand" href=${this.href}>
          <slot name="logo"></slot>
          ${this.siteTitle ? l`<span>${this.siteTitle}</span>` : x}
        </a>
        <button
          type="button"
          class="menu-toggle"
          aria-expanded=${this.mobileOpen ? "true" : "false"}
          aria-controls="nav-wrap"
          aria-label=${this.mobileOpen ? "Close menu" : "Open menu"}
          @click=${this.toggleMobileMenu}
        >
          <svg
            class="menu-icon"
            viewBox="0 0 48 48"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
            aria-hidden="true"
          >
            ${this.mobileOpen ? V.close : V.menu}
          </svg>
        </button>
        <div id="nav-wrap" class="nav-wrap${this.mobileOpen ? " open" : ""}">
          <nav aria-label="Main">
            <slot @click=${this.closeMobileMenu}></slot>
          </nav>
          <div class="actions">
            <slot name="actions"></slot>
          </div>
        </div>
      </header>
    `;
  }
};
X.styles = d`
    :host {
      display: block;
      font-family: var(--vox-font-family-base);
      background-color: var(--vox-color-bg);
      border-bottom: 1px solid var(--vox-color-divider);
    }

    .header {
      display: flex;
      align-items: center;
      gap: var(--vox-space-6);
      flex-wrap: wrap;
      max-width: 1280px;
      margin: 0 auto;
      padding: var(--vox-space-3) var(--vox-space-6);
    }

    .brand {
      display: inline-flex;
      align-items: center;
      gap: var(--vox-space-2);
      color: var(--vox-color-text-1);
      font-size: 16px;
      font-weight: 700;
      text-decoration: none;
    }

    .brand ::slotted(img),
    .brand ::slotted(svg) {
      height: 28px;
      width: auto;
    }

    .menu-toggle {
      display: none;
      align-items: center;
      justify-content: center;
      width: 36px;
      height: 36px;
      margin-left: auto;
      padding: 0;
      background: none;
      border: none;
      border-radius: var(--vox-radius-sm);
      color: var(--vox-color-text-1);
      cursor: pointer;
    }

    .menu-toggle:hover {
      color: var(--vox-color-brand-1);
    }

    .menu-toggle:focus-visible {
      outline: 2px solid var(--vox-color-brand-1);
      outline-offset: 2px;
    }

    .menu-icon {
      width: 22px;
      height: 22px;
    }

    .nav-wrap {
      display: contents;
    }

    nav {
      display: flex;
      align-items: center;
      gap: var(--vox-space-4);
      flex-wrap: wrap;
      flex: 1 1 auto;
    }

    nav ::slotted(a) {
      color: var(--vox-color-text-2);
      font-size: 14px;
      font-weight: 500;
      text-decoration: none;
      transition: color var(--vox-transition-fast);
    }

    nav ::slotted(a:hover) {
      color: var(--vox-color-brand-1);
    }

    nav ::slotted(a[aria-current='page']) {
      color: var(--vox-color-brand-1);
      font-weight: 600;
    }

    .actions {
      display: flex;
      align-items: center;
      gap: var(--vox-space-3);
    }

    @media (max-width: 768px) {
      .menu-toggle {
        display: inline-flex;
      }

      .nav-wrap {
        display: none;
        width: 100%;
      }

      .nav-wrap.open {
        display: flex;
        flex-direction: column;
        align-items: stretch;
        gap: var(--vox-space-4);
        margin-top: var(--vox-space-3);
        padding-top: var(--vox-space-4);
        border-top: 1px solid var(--vox-color-divider);
      }

      nav {
        flex-direction: column;
        align-items: flex-start;
        gap: var(--vox-space-3);
      }

      .actions {
        flex-direction: column;
        align-items: stretch;
      }
    }
  `;
ht([
  n({ attribute: "site-title" })
], X.prototype, "siteTitle", 2);
ht([
  n()
], X.prototype, "href", 2);
ht([
  nt()
], X.prototype, "mobileOpen", 2);
X = ht([
  h("vox-header")
], X);
var Sr = Object.defineProperty, jr = Object.getOwnPropertyDescriptor, He = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? jr(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && Sr(e, o, t), t;
};
let H = class extends p {
  constructor() {
    super(...arguments), this.previousLabel = "", this.nextLabel = "";
  }
  render() {
    return l`
      <nav aria-label="Series">
        ${this.previousHref ? l`
              <a href=${this.previousHref} rel="prev">
                <span class="direction">← Previous</span>
                <span class="title">${this.previousLabel}</span>
              </a>
            ` : x}
        ${this.nextHref ? l`
              <a class="next" href=${this.nextHref} rel="next">
                <span class="direction">Next →</span>
                <span class="title">${this.nextLabel}</span>
              </a>
            ` : x}
      </nav>
    `;
  }
};
H.styles = d`
    :host {
      display: block;
      font-family: var(--vox-font-family-base);
    }

    nav {
      display: flex;
      justify-content: space-between;
      gap: var(--vox-space-4);
    }

    a {
      display: flex;
      flex-direction: column;
      gap: 2px;
      flex: 0 1 48%;
      padding: var(--vox-space-3) var(--vox-space-4);
      border: 1px solid var(--vox-color-divider);
      border-radius: var(--vox-radius-md);
      text-decoration: none;
      transition: border-color var(--vox-transition-base);
    }

    a:hover,
    a:focus-visible {
      border-color: var(--vox-color-brand-1);
    }

    a:focus-visible {
      outline: 2px solid var(--vox-color-brand-1);
      outline-offset: 2px;
    }

    .next {
      margin-left: auto;
      text-align: right;
    }

    .direction {
      font-size: 12px;
      color: var(--vox-color-text-3);
    }

    .title {
      font-size: 14px;
      font-weight: 600;
      color: var(--vox-color-brand-1);
    }
  `;
He([
  n({ attribute: "previous-href" })
], H.prototype, "previousHref", 2);
He([
  n({ attribute: "previous-label" })
], H.prototype, "previousLabel", 2);
He([
  n({ attribute: "next-href" })
], H.prototype, "nextHref", 2);
He([
  n({ attribute: "next-label" })
], H.prototype, "nextLabel", 2);
H = He([
  h("vox-series-nav")
], H);
var Vr = Object.defineProperty, Dr = Object.getOwnPropertyDescriptor, $ = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? Dr(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && Vr(e, o, t), t;
};
let J = class extends p {
  constructor() {
    super(...arguments), this.label = "Section", this.toggleLabel = "Menu", this.mobileOpen = !1, this.handleOutsideClick = (r) => {
      this.mobileOpen && !r.composedPath().includes(this) && (this.mobileOpen = !1);
    }, this.handleKeydown = (r) => {
      var e;
      r.key === "Escape" && this.mobileOpen && (this.mobileOpen = !1, (e = this.renderRoot.querySelector(".toggle")) == null || e.focus());
    }, this.toggleMobile = () => {
      this.mobileOpen = !this.mobileOpen;
    }, this.handleNavClick = (r) => {
      r.composedPath().some((e) => e instanceof HTMLAnchorElement) && (this.mobileOpen = !1);
    };
  }
  connectedCallback() {
    super.connectedCallback(), document.addEventListener("click", this.handleOutsideClick), this.addEventListener("keydown", this.handleKeydown);
  }
  disconnectedCallback() {
    super.disconnectedCallback(), document.removeEventListener("click", this.handleOutsideClick), this.removeEventListener("keydown", this.handleKeydown);
  }
  render() {
    return l`
      <button
        type="button"
        class="toggle"
        aria-expanded=${this.mobileOpen ? "true" : "false"}
        aria-controls="nav"
        @click=${this.toggleMobile}
      >
        ${this.toggleLabel}
        <svg
          class="toggle-icon"
          viewBox="0 0 48 48"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
        >
          ${this.mobileOpen ? V.close : V.menu}
        </svg>
      </button>
      <nav
        id="nav"
        class=${this.mobileOpen ? "open" : ""}
        aria-label=${this.label}
        @click=${this.handleNavClick}
      >
        <slot></slot>
      </nav>
    `;
  }
};
J.styles = d`
    :host {
      display: block;
      font-family: var(--vox-font-family-base);
    }

    .toggle {
      display: none;
      align-items: center;
      justify-content: space-between;
      width: 100%;
      gap: var(--vox-space-2);
      padding: var(--vox-space-2) var(--vox-space-3);
      background: none;
      border: 1px solid var(--vox-color-divider);
      border-radius: var(--vox-radius-md);
      color: var(--vox-color-text-1);
      font-family: inherit;
      font-size: 14px;
      font-weight: 600;
      cursor: pointer;
    }

    .toggle:hover {
      border-color: var(--vox-color-brand-1);
      color: var(--vox-color-brand-1);
    }

    .toggle:focus-visible {
      outline: 2px solid var(--vox-color-brand-1);
      outline-offset: 2px;
    }

    .toggle-icon {
      width: 18px;
      height: 18px;
      flex: none;
    }

    nav {
      display: flex;
      flex-direction: column;
      gap: 2px;
    }

    @media (max-width: 768px) {
      .toggle {
        display: flex;
      }

      nav {
        display: none;
        margin-top: var(--vox-space-2);
      }

      nav.open {
        display: flex;
      }
    }
  `;
$([
  n()
], J.prototype, "label", 2);
$([
  n({ attribute: "toggle-label" })
], J.prototype, "toggleLabel", 2);
$([
  nt()
], J.prototype, "mobileOpen", 2);
J = $([
  h("vox-sidenav")
], J);
let fe = class extends p {
  constructor() {
    super(...arguments), this.heading = "", this.open = !1;
  }
  toggle() {
    this.open = !this.open;
  }
  render() {
    return l`
      <button
        class="trigger"
        aria-expanded=${this.open ? "true" : "false"}
        @click=${this.toggle}
      >
        ${this.heading}
        <svg class="chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="m9 6 6 6-6 6" />
        </svg>
      </button>
      <div class="items"><slot></slot></div>
    `;
  }
};
fe.styles = d`
    :host {
      display: block;
      font-family: var(--vox-font-family-base);
    }

    .trigger {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: var(--vox-space-2);
      width: 100%;
      padding: var(--vox-space-2) var(--vox-space-3);
      background: none;
      border: none;
      border-radius: var(--vox-radius-sm);
      color: var(--vox-color-text-1);
      font-family: inherit;
      font-size: 14px;
      font-weight: 600;
      text-align: left;
      cursor: pointer;
    }

    .trigger:hover {
      color: var(--vox-color-brand-1);
    }

    .trigger:focus-visible {
      outline: 2px solid var(--vox-color-brand-1);
      outline-offset: -2px;
    }

    .chevron {
      flex: none;
      width: 14px;
      height: 14px;
      transition: transform var(--vox-transition-fast);
    }

    :host([open]) .chevron {
      transform: rotate(90deg);
    }

    .items {
      display: none;
      flex-direction: column;
      gap: 2px;
      padding-left: var(--vox-space-3);
      border-left: 1px solid var(--vox-color-divider);
      margin-left: var(--vox-space-3);
    }

    :host([open]) .items {
      display: flex;
    }
  `;
$([
  n()
], fe.prototype, "heading", 2);
$([
  n({ type: Boolean, reflect: !0 })
], fe.prototype, "open", 2);
fe = $([
  h("vox-sidenav-group")
], fe);
let ge = class extends p {
  constructor() {
    super(...arguments), this.href = "#", this.current = !1, this.hasIcon = !1;
  }
  handleIconSlotChange(r) {
    const e = r.target;
    this.hasIcon = e.assignedNodes({ flatten: !0 }).length > 0, this.requestUpdate();
  }
  render() {
    return l`
      <a href=${this.href} aria-current=${this.current ? "page" : "false"}>
        <span class="icon ${this.hasIcon ? "has-icon" : ""}">
          <slot name="icon" @slotchange=${this.handleIconSlotChange}></slot>
        </span>
        <slot></slot>
      </a>
    `;
  }
};
ge.styles = d`
    :host {
      display: block;
      font-family: var(--vox-font-family-base);
    }

    a {
      display: flex;
      align-items: center;
      gap: var(--vox-space-2);
      padding: var(--vox-space-2) var(--vox-space-3);
      border-radius: var(--vox-radius-sm);
      color: var(--vox-color-text-2);
      font-size: 14px;
      text-decoration: none;
      transition:
        color var(--vox-transition-fast),
        background-color var(--vox-transition-fast);
    }

    a:hover {
      color: var(--vox-color-text-1);
    }

    a:focus-visible {
      outline: 2px solid var(--vox-color-brand-1);
      outline-offset: -2px;
    }

    :host([current]) a {
      background-color: var(--vox-color-brand-soft);
      color: var(--vox-color-brand-1);
      font-weight: 600;
    }

    .icon {
      display: flex;
      flex: none;
    }

    .icon:not(.has-icon) {
      display: none;
    }
  `;
$([
  n()
], ge.prototype, "href", 2);
$([
  n({ type: Boolean, reflect: !0 })
], ge.prototype, "current", 2);
ge = $([
  h("vox-sidenav-item")
], ge);
var zr = Object.defineProperty, Hr = Object.getOwnPropertyDescriptor, yo = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? Hr(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && zr(e, o, t), t;
};
let Xe = class extends p {
  constructor() {
    super(...arguments), this.label = "Secondary";
  }
  render() {
    return l`
      <nav aria-label=${this.label}>
        <slot></slot>
      </nav>
    `;
  }
};
Xe.styles = d`
    :host {
      display: block;
      font-family: var(--vox-font-family-base);
    }

    nav {
      display: flex;
      gap: var(--vox-space-1);
      overflow-x: auto;
      border-bottom: 1px solid var(--vox-color-divider);
    }

    ::slotted(a) {
      padding: var(--vox-space-2) var(--vox-space-4);
      margin-bottom: -1px;
      border-bottom: 2px solid transparent;
      color: var(--vox-color-text-2);
      font-size: 14px;
      font-weight: 500;
      text-decoration: none;
      white-space: nowrap;
      transition: color var(--vox-transition-fast);
    }

    ::slotted(a:hover) {
      color: var(--vox-color-text-1);
    }

    ::slotted(a[aria-current='page']) {
      color: var(--vox-color-brand-1);
      font-weight: 600;
      border-bottom-color: var(--vox-color-brand-1);
    }
  `;
yo([
  n()
], Xe.prototype, "label", 2);
Xe = yo([
  h("vox-subnav")
], Xe);
var Tr = Object.defineProperty, Br = Object.getOwnPropertyDescriptor, I = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? Br(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && Tr(e, o, t), t;
};
let _t = class extends p {
  get tabs() {
    return [...this.querySelectorAll("vox-tab")];
  }
  get panels() {
    return [...this.querySelectorAll("vox-tab-panel")];
  }
  sync() {
    const r = this.tabs;
    r.length > 0 && !r.some((o) => o.selected) && (r[0].selected = !0);
    const e = r.find((o) => o.selected);
    this.panels.forEach((o) => {
      o.active = o.name === (e == null ? void 0 : e.panel);
    });
  }
  handleSelect(r) {
    const e = r.target;
    e.tagName === "VOX-TAB" && (this.tabs.forEach((o) => o.selected = o === e), this.sync(), this.dispatchEvent(
      new CustomEvent("vox-tab-change", {
        detail: { panel: e.panel },
        bubbles: !0,
        composed: !0
      })
    ));
  }
  handleKeydown(r) {
    const o = { ArrowRight: 1, ArrowLeft: -1 }[r.key];
    if (!o) return;
    r.preventDefault();
    const s = this.tabs, t = s.findIndex((i) => i.selected), a = s[(t + o + s.length) % s.length];
    a.select(), a.focusTab();
  }
  render() {
    return l`
      <div
        class="tablist"
        role="tablist"
        @vox-tab-select=${this.handleSelect}
        @keydown=${this.handleKeydown}
      >
        <slot name="tab" @slotchange=${this.sync}></slot>
      </div>
      <slot @slotchange=${this.sync}></slot>
    `;
  }
};
_t.styles = d`
    :host {
      display: block;
    }

    .tablist {
      display: flex;
      gap: var(--vox-space-1);
      border-bottom: 1px solid var(--vox-color-divider);
    }
  `;
_t = I([
  h("vox-tabs")
], _t);
let be = class extends p {
  constructor() {
    super(...arguments), this.panel = "", this.selected = !1;
  }
  select() {
    this.dispatchEvent(
      new CustomEvent("vox-tab-select", { bubbles: !0, composed: !0 })
    );
  }
  focusTab() {
    var r;
    (r = this.renderRoot.querySelector(".tab")) == null || r.focus();
  }
  render() {
    return l`
      <button
        class="tab"
        role="tab"
        aria-selected=${this.selected ? "true" : "false"}
        tabindex=${this.selected ? "0" : "-1"}
        @click=${this.select}
      >
        <slot></slot>
      </button>
    `;
  }
};
be.styles = d`
    :host {
      display: block;
      font-family: var(--vox-font-family-base);
    }

    .tab {
      padding: var(--vox-space-2) var(--vox-space-4);
      margin-bottom: -1px;
      background: none;
      border: none;
      border-bottom: 2px solid transparent;
      color: var(--vox-color-text-2);
      font-family: inherit;
      font-size: 14px;
      font-weight: 600;
      cursor: pointer;
      transition: color var(--vox-transition-fast);
    }

    .tab:hover {
      color: var(--vox-color-text-1);
    }

    .tab:focus-visible {
      outline: 2px solid var(--vox-color-brand-1);
      outline-offset: -2px;
      border-radius: var(--vox-radius-sm);
    }

    :host([selected]) .tab {
      color: var(--vox-color-brand-1);
      border-bottom-color: var(--vox-color-brand-1);
    }
  `;
I([
  n()
], be.prototype, "panel", 2);
I([
  n({ type: Boolean, reflect: !0 })
], be.prototype, "selected", 2);
be = I([
  h("vox-tab")
], be);
let me = class extends p {
  constructor() {
    super(...arguments), this.name = "", this.active = !1;
  }
  render() {
    return l`<div role="tabpanel"><slot></slot></div>`;
  }
};
me.styles = d`
    :host {
      display: none;
      font-family: var(--vox-font-family-base);
      font-size: 14px;
      line-height: 1.7;
      color: var(--vox-color-text-2);
      padding: var(--vox-space-4) 0;
    }

    :host([active]) {
      display: block;
    }
  `;
I([
  n()
], me.prototype, "name", 2);
I([
  n({ type: Boolean, reflect: !0 })
], me.prototype, "active", 2);
me = I([
  h("vox-tab-panel")
], me);
var Nr = Object.defineProperty, Rr = Object.getOwnPropertyDescriptor, Te = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? Rr(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && Nr(e, o, t), t;
};
let Je = class extends p {
  constructor() {
    super(...arguments), this.heading = "On this page";
  }
  render() {
    return l`
      <div class="heading" id="vox-toc-heading">${this.heading}</div>
      <nav aria-labelledby="vox-toc-heading">
        <slot></slot>
      </nav>
    `;
  }
};
Je.styles = d`
    :host {
      display: block;
      font-family: var(--vox-font-family-base);
    }

    .heading {
      margin: 0 0 var(--vox-space-2);
      padding-left: var(--vox-space-3);
      font-size: 13px;
      font-weight: 600;
      color: var(--vox-color-text-1);
    }

    nav {
      display: flex;
      flex-direction: column;
      gap: 1px;
      border-left: 1px solid var(--vox-color-divider);
    }
  `;
Te([
  n()
], Je.prototype, "heading", 2);
Je = Te([
  h("vox-toc")
], Je);
let ye = class extends p {
  constructor() {
    super(...arguments), this.href = "#", this.current = !1, this.hasChildren = !1;
  }
  handleChildrenSlotChange(r) {
    const e = r.target;
    this.hasChildren = e.assignedNodes({ flatten: !0 }).length > 0, this.requestUpdate();
  }
  render() {
    return l`
      <a href=${this.href} aria-current=${this.current ? "true" : "false"}>
        <slot></slot>
      </a>
      <div class="children ${this.hasChildren ? "has-children" : ""}">
        <slot name="children" @slotchange=${this.handleChildrenSlotChange}></slot>
      </div>
    `;
  }
};
ye.styles = d`
    :host {
      display: block;
      font-family: var(--vox-font-family-base);
    }

    a {
      display: block;
      margin-left: -1px;
      padding: var(--vox-space-1) var(--vox-space-3);
      border-left: 2px solid transparent;
      color: var(--vox-color-text-2);
      font-size: 13px;
      line-height: 1.5;
      text-decoration: none;
      transition:
        color var(--vox-transition-fast),
        border-color var(--vox-transition-fast);
    }

    a:hover {
      color: var(--vox-color-text-1);
    }

    a:focus-visible {
      outline: 2px solid var(--vox-color-brand-1);
      outline-offset: -2px;
    }

    :host([current]) a {
      border-left-color: var(--vox-color-brand-1);
      color: var(--vox-color-brand-1);
      font-weight: 600;
    }

    .children {
      display: flex;
      flex-direction: column;
      gap: 1px;
      padding-left: var(--vox-space-3);
    }

    .children:not(.has-children) {
      display: none;
    }
  `;
Te([
  n()
], ye.prototype, "href", 2);
Te([
  n({ type: Boolean, reflect: !0 })
], ye.prototype, "current", 2);
ye = Te([
  h("vox-toc-item")
], ye);
var Ir = Object.defineProperty, Ur = Object.getOwnPropertyDescriptor, Be = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? Ur(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && Ir(e, o, t), t;
};
let T = class extends p {
  constructor() {
    super(...arguments), this.heading = "", this.open = !1, this.lightDismiss = !1, this.hasFooter = !1;
  }
  show() {
    this.open = !0;
  }
  close() {
    this.open = !1;
  }
  updated() {
    this.open && !this.dialogEl.open ? this.dialogEl.showModal() : !this.open && this.dialogEl.open && this.dialogEl.close();
  }
  handleNativeClose() {
    this.open = !1, this.dispatchEvent(
      new CustomEvent("vox-close", { bubbles: !0, composed: !0 })
    );
  }
  handleClick(r) {
    this.lightDismiss && r.target === this.dialogEl && this.close();
  }
  handleFooterSlotChange(r) {
    const e = r.target;
    this.hasFooter = e.assignedNodes({ flatten: !0 }).length > 0, this.requestUpdate();
  }
  render() {
    return l`
      <dialog
        aria-label=${this.heading || x}
        @close=${this.handleNativeClose}
        @click=${this.handleClick}
      >
        <div class="header">
          <h2 class="heading">${this.heading}</h2>
          <button class="close" aria-label="Close dialog" @click=${this.close}>
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" aria-hidden="true">
              <path d="M18 6 6 18M6 6l12 12" />
            </svg>
          </button>
        </div>
        <div class="body"><slot></slot></div>
        <div class="footer ${this.hasFooter ? "has-content" : ""}">
          <slot name="footer" @slotchange=${this.handleFooterSlotChange}></slot>
        </div>
      </dialog>
    `;
  }
};
T.styles = d`
    dialog {
      box-sizing: border-box;
      width: min(90vw, 480px);
      padding: 0;
      border: 1px solid var(--vox-color-divider);
      border-radius: var(--vox-radius-lg);
      background-color: var(--vox-color-bg-elv);
      color: var(--vox-color-text-1);
      font-family: var(--vox-font-family-base);
      box-shadow: var(--vox-shadow-2);
    }

    dialog::backdrop {
      background-color: rgba(0, 0, 0, 0.5);
    }

    .header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: var(--vox-space-3);
      padding: var(--vox-space-4) var(--vox-space-6);
      border-bottom: 1px solid var(--vox-color-divider);
    }

    .heading {
      margin: 0;
      font-size: 16px;
      font-weight: 600;
    }

    .close {
      display: flex;
      align-items: center;
      justify-content: center;
      width: 28px;
      height: 28px;
      padding: 0;
      background: none;
      border: none;
      border-radius: var(--vox-radius-sm);
      color: var(--vox-color-text-2);
      cursor: pointer;
    }

    .close:hover {
      color: var(--vox-color-text-1);
      background-color: var(--vox-color-bg-soft);
    }

    .close:focus-visible {
      outline: 2px solid var(--vox-color-brand-1);
    }

    .close svg {
      width: 16px;
      height: 16px;
    }

    .body {
      padding: var(--vox-space-6);
      font-size: 14px;
      line-height: 1.7;
      color: var(--vox-color-text-2);
    }

    .footer {
      display: flex;
      justify-content: flex-end;
      gap: var(--vox-space-3);
      padding: var(--vox-space-4) var(--vox-space-6);
      border-top: 1px solid var(--vox-color-divider);
    }

    .footer:not(.has-content) {
      display: none;
    }

    ::slotted(p:first-child) {
      margin-top: 0;
    }

    ::slotted(p:last-child) {
      margin-bottom: 0;
    }
  `;
Be([
  n()
], T.prototype, "heading", 2);
Be([
  n({ type: Boolean })
], T.prototype, "open", 2);
Be([
  n({ type: Boolean, attribute: "light-dismiss" })
], T.prototype, "lightDismiss", 2);
Be([
  jt("dialog")
], T.prototype, "dialogEl", 2);
T = Be([
  h("vox-dialog")
], T);
var qr = Object.defineProperty, Fr = Object.getOwnPropertyDescriptor, Bt = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? Fr(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && qr(e, o, t), t;
};
let $e = class extends p {
  constructor() {
    super(...arguments), this.summary = "Show details", this.open = !1;
  }
  toggle() {
    this.open = !this.open, this.dispatchEvent(
      new CustomEvent("vox-toggle", { bubbles: !0, composed: !0 })
    );
  }
  render() {
    return l`
      <button
        class="trigger"
        aria-expanded=${this.open ? "true" : "false"}
        @click=${this.toggle}
      >
        <svg class="chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="m9 6 6 6-6 6" />
        </svg>
        ${this.summary}
      </button>
      <div class="panel" ?hidden=${!this.open}>
        <slot></slot>
      </div>
    `;
  }
};
$e.styles = d`
    :host {
      display: block;
      font-family: var(--vox-font-family-base);
    }

    .trigger {
      display: inline-flex;
      align-items: center;
      gap: var(--vox-space-2);
      padding: 0;
      background: none;
      border: none;
      color: var(--vox-color-brand-1);
      font-family: inherit;
      font-size: 14px;
      font-weight: 600;
      cursor: pointer;
    }

    .trigger:hover {
      color: var(--vox-color-brand-2);
      text-decoration: underline;
    }

    .trigger:focus-visible {
      outline: 2px solid var(--vox-color-brand-1);
      outline-offset: 2px;
      border-radius: var(--vox-radius-sm);
    }

    .chevron {
      width: 14px;
      height: 14px;
      transition: transform var(--vox-transition-fast);
    }

    :host([open]) .chevron {
      transform: rotate(90deg);
    }

    .panel {
      margin-top: var(--vox-space-3);
      font-size: 14px;
      line-height: 1.7;
      color: var(--vox-color-text-2);
    }

    .panel[hidden] {
      display: none;
    }
  `;
Bt([
  n()
], $e.prototype, "summary", 2);
Bt([
  n({ type: Boolean, reflect: !0 })
], $e.prototype, "open", 2);
$e = Bt([
  h("vox-disclosure")
], $e);
var Zr = Object.defineProperty, Kr = Object.getOwnPropertyDescriptor, Nt = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? Kr(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && Zr(e, o, t), t;
};
let we = class extends p {
  constructor() {
    super(...arguments), this.label = "Menu", this.open = !1, this.handleOutsideClick = (r) => {
      this.open && !r.composedPath().includes(this) && (this.open = !1);
    }, this.handleKeydown = (r) => {
      var e;
      if (r.key === "Escape" && this.open) {
        this.open = !1, (e = this.renderRoot.querySelector(".trigger")) == null || e.focus();
        return;
      }
      if ((r.key === "ArrowDown" || r.key === "ArrowUp") && this.open) {
        r.preventDefault();
        const o = [...this.querySelectorAll("a, button")];
        if (o.length === 0) return;
        const s = document.activeElement, t = o.indexOf(s), a = r.key === "ArrowDown" ? 1 : -1;
        o[(Math.max(t, 0) + a + o.length) % o.length].focus();
      }
    };
  }
  connectedCallback() {
    super.connectedCallback(), document.addEventListener("click", this.handleOutsideClick), this.addEventListener("keydown", this.handleKeydown);
  }
  disconnectedCallback() {
    super.disconnectedCallback(), document.removeEventListener("click", this.handleOutsideClick), this.removeEventListener("keydown", this.handleKeydown);
  }
  toggle() {
    this.open = !this.open;
  }
  render() {
    return l`
      <button
        class="trigger"
        aria-expanded=${this.open ? "true" : "false"}
        aria-haspopup="true"
        @click=${this.toggle}
      >
        ${this.label}
        <svg class="chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="m6 9 6 6 6-6" />
        </svg>
      </button>
      <div class="menu">
        <slot @click=${() => this.open = !1}></slot>
      </div>
    `;
  }
};
we.styles = d`
    :host {
      position: relative;
      display: inline-block;
      /* Without this, a flex/grid container's default stretch alignment
         grows the host to fill the cross axis while the trigger button
         inside stays content-sized — and since the menu's "top: 100%" is
         measured against the host's own box, it then opens far below the
         trigger instead of right under it. */
      align-self: flex-start;
      font-family: var(--vox-font-family-base);
    }

    .trigger {
      display: inline-flex;
      align-items: center;
      gap: var(--vox-space-2);
      padding: 0 var(--vox-space-4);
      height: 38px;
      background-color: var(--vox-color-bg-soft);
      border: 1px solid var(--vox-color-divider);
      border-radius: var(--vox-radius-md);
      color: var(--vox-color-text-1);
      font-family: inherit;
      font-size: 14px;
      font-weight: 600;
      cursor: pointer;
      transition:
        border-color var(--vox-transition-fast),
        color var(--vox-transition-fast);
    }

    .trigger:hover {
      border-color: var(--vox-color-brand-1);
      color: var(--vox-color-brand-1);
    }

    .trigger:focus-visible {
      outline: 2px solid var(--vox-color-brand-1);
      outline-offset: 2px;
    }

    .chevron {
      width: 14px;
      height: 14px;
      transition: transform var(--vox-transition-fast);
    }

    :host([open]) .chevron {
      transform: rotate(180deg);
    }

    .menu {
      position: absolute;
      top: calc(100% + 4px);
      left: 0;
      z-index: 10;
      min-width: max(100%, 180px);
      display: none;
      flex-direction: column;
      padding: var(--vox-space-2);
      background-color: var(--vox-color-bg-elv);
      border: 1px solid var(--vox-color-divider);
      border-radius: var(--vox-radius-md);
      box-shadow: var(--vox-shadow-2);
    }

    :host([open]) .menu {
      display: flex;
    }

    ::slotted(a),
    ::slotted(button) {
      display: block;
      width: 100%;
      box-sizing: border-box;
      padding: var(--vox-space-2) var(--vox-space-3);
      background: none;
      border: none;
      border-radius: var(--vox-radius-sm);
      color: var(--vox-color-text-1);
      font-family: inherit;
      font-size: 14px;
      text-align: left;
      text-decoration: none;
      cursor: pointer;
      white-space: nowrap;
    }

    ::slotted(a:hover),
    ::slotted(button:hover),
    ::slotted(a:focus-visible),
    ::slotted(button:focus-visible) {
      background-color: var(--vox-color-brand-soft);
      color: var(--vox-color-brand-1);
      outline: none;
    }

    ::slotted(hr) {
      width: 100%;
      margin: var(--vox-space-1) 0;
      border: none;
      border-top: 1px solid var(--vox-color-divider);
    }
  `;
Nt([
  n()
], we.prototype, "label", 2);
Nt([
  n({ type: Boolean, reflect: !0 })
], we.prototype, "open", 2);
we = Nt([
  h("vox-dropdown")
], we);
var Wr = Object.defineProperty, Gr = Object.getOwnPropertyDescriptor, pt = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? Gr(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && Wr(e, o, t), t;
};
let Y = class extends p {
  constructor() {
    super(...arguments), this.placement = "bottom-end", this.open = !1, this.handleOutsideClick = (r) => {
      this.open && !r.composedPath().includes(this) && this.close();
    }, this.handleKeydown = (r) => {
      var e;
      if (r.key === "Escape" && this.open) {
        this.close(), (e = this.renderRoot.querySelector(".trigger")) == null || e.focus();
        return;
      }
      if ((r.key === "ArrowDown" || r.key === "ArrowUp") && this.open) {
        r.preventDefault();
        const o = [...this.querySelectorAll("a, button")].filter(
          (u) => u.slot !== "trigger"
        );
        if (o.length === 0) return;
        const s = document.activeElement, t = o.indexOf(s), a = r.key === "ArrowDown" ? 1 : -1;
        o[(Math.max(t, 0) + a + o.length) % o.length].focus();
      }
    };
  }
  connectedCallback() {
    super.connectedCallback(), document.addEventListener("click", this.handleOutsideClick), this.addEventListener("keydown", this.handleKeydown);
  }
  disconnectedCallback() {
    super.disconnectedCallback(), document.removeEventListener("click", this.handleOutsideClick), this.removeEventListener("keydown", this.handleKeydown);
  }
  toggle() {
    this.open ? this.close() : this.open = !0;
  }
  close() {
    this.open && (this.open = !1, this.dispatchEvent(new CustomEvent("vox-close", { bubbles: !0, composed: !0 })));
  }
  render() {
    return l`
      <button
        class="trigger"
        aria-expanded=${this.open ? "true" : "false"}
        aria-haspopup="true"
        aria-label=${this.label ?? x}
        @click=${this.toggle}
      >
        <slot name="trigger"></slot>
      </button>
      <div class="menu" role="menu">
        <slot @click=${() => this.close()}></slot>
      </div>
    `;
  }
};
Y.styles = d`
    :host {
      position: relative;
      display: inline-block;
      /* Without this, a flex/grid container's default stretch alignment
         grows the host to fill the cross axis (e.g. a tall sibling, or a
         container given a min-height for layout purposes) while the
         trigger button inside stays content-sized — and since the menu's
         "top: 100%" is measured against the host's own box, it then opens
         far below the trigger instead of right under it. */
      align-self: flex-start;
      font-family: var(--vox-font-family-base);
    }

    .trigger {
      display: inline-flex;
      align-items: center;
      background: none;
      border: none;
      padding: 0;
      border-radius: var(--vox-radius-md);
      color: inherit;
      font: inherit;
      cursor: pointer;
    }

    .trigger:focus-visible {
      outline: 2px solid var(--vox-color-brand-1);
      outline-offset: 2px;
    }

    .menu {
      position: absolute;
      top: calc(100% + 4px);
      z-index: 10;
      min-width: 180px;
      display: none;
      flex-direction: column;
      padding: var(--vox-space-2);
      background-color: var(--vox-color-bg-elv);
      border: 1px solid var(--vox-color-divider);
      border-radius: var(--vox-radius-md);
      box-shadow: var(--vox-shadow-2);
    }

    :host([placement='bottom-start']) .menu {
      left: 0;
    }

    :host([placement='bottom-end']) .menu {
      right: 0;
    }

    :host([open]) .menu {
      display: flex;
    }

    ::slotted(a),
    ::slotted(button) {
      display: block;
      width: 100%;
      box-sizing: border-box;
      padding: var(--vox-space-2) var(--vox-space-3);
      background: none;
      border: none;
      border-radius: var(--vox-radius-sm);
      color: var(--vox-color-text-1);
      font-family: inherit;
      font-size: 14px;
      text-align: left;
      text-decoration: none;
      cursor: pointer;
      white-space: nowrap;
    }

    ::slotted(a:hover),
    ::slotted(button:hover),
    ::slotted(a:focus-visible),
    ::slotted(button:focus-visible) {
      background-color: var(--vox-color-brand-soft);
      color: var(--vox-color-brand-1);
      outline: none;
    }

    ::slotted(hr) {
      width: 100%;
      margin: var(--vox-space-1) 0;
      border: none;
      border-top: 1px solid var(--vox-color-divider);
    }
  `;
pt([
  n()
], Y.prototype, "label", 2);
pt([
  n({ reflect: !0 })
], Y.prototype, "placement", 2);
pt([
  n({ type: Boolean, reflect: !0 })
], Y.prototype, "open", 2);
Y = pt([
  h("vox-menu")
], Y);
var Xr = Object.defineProperty, Jr = Object.getOwnPropertyDescriptor, Ne = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? Jr(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && Xr(e, o, t), t;
};
let Ye = class extends p {
  constructor() {
    super(...arguments), this.single = !1;
  }
  handleToggle(r) {
    if (!this.single) return;
    const e = r.target;
    e.open && this.querySelectorAll("vox-accordion-item").forEach(
      (o) => {
        o !== e && (o.open = !1);
      }
    );
  }
  render() {
    return l`<slot @vox-toggle=${this.handleToggle}></slot>`;
  }
};
Ye.styles = d`
    :host {
      display: block;
      border: 1px solid var(--vox-color-divider);
      border-radius: var(--vox-radius-md);
      overflow: hidden;
    }

    ::slotted(vox-accordion-item:not(:first-child)) {
      border-top: 1px solid var(--vox-color-divider);
    }
  `;
Ne([
  n({ type: Boolean })
], Ye.prototype, "single", 2);
Ye = Ne([
  h("vox-accordion")
], Ye);
let _e = class extends p {
  constructor() {
    super(...arguments), this.heading = "", this.open = !1;
  }
  toggle() {
    this.open = !this.open, this.dispatchEvent(
      new CustomEvent("vox-toggle", { bubbles: !0, composed: !0 })
    );
  }
  render() {
    return l`
      <h3 style="margin:0">
        <button
          class="trigger"
          aria-expanded=${this.open ? "true" : "false"}
          @click=${this.toggle}
        >
          ${this.heading}
          <svg class="chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d="m6 9 6 6 6-6" />
          </svg>
        </button>
      </h3>
      <div class="panel" ?hidden=${!this.open}>
        <slot></slot>
      </div>
    `;
  }
};
_e.styles = d`
    :host {
      display: block;
      font-family: var(--vox-font-family-base);
    }

    .trigger {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: var(--vox-space-3);
      width: 100%;
      padding: var(--vox-space-4);
      background: none;
      border: none;
      color: var(--vox-color-text-1);
      font-family: inherit;
      font-size: 15px;
      font-weight: 600;
      text-align: left;
      cursor: pointer;
    }

    .trigger:hover {
      color: var(--vox-color-brand-1);
    }

    .trigger:focus-visible {
      outline: 2px solid var(--vox-color-brand-1);
      outline-offset: -2px;
    }

    .chevron {
      flex: none;
      width: 16px;
      height: 16px;
      transition: transform var(--vox-transition-fast);
    }

    :host([open]) .chevron {
      transform: rotate(180deg);
    }

    .panel {
      padding: 0 var(--vox-space-4) var(--vox-space-4);
      font-size: 14px;
      line-height: 1.7;
      color: var(--vox-color-text-2);
    }

    .panel[hidden] {
      display: none;
    }

    ::slotted(p:first-child) {
      margin-top: 0;
    }

    ::slotted(p:last-child) {
      margin-bottom: 0;
    }
  `;
Ne([
  n()
], _e.prototype, "heading", 2);
Ne([
  n({ type: Boolean, reflect: !0 })
], _e.prototype, "open", 2);
_e = Ne([
  h("vox-accordion-item")
], _e);
var Yr = Object.defineProperty, Qr = Object.getOwnPropertyDescriptor, Re = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? Qr(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && Yr(e, o, t), t;
};
const es = {
  info: "info",
  success: "check-circle",
  warning: "warning",
  danger: "x-circle"
};
let B = class extends p {
  constructor() {
    super(...arguments), this.variant = "info", this.heading = "", this.dismissible = !1, this.open = !0;
  }
  dismiss() {
    this.open = !1, this.dispatchEvent(
      new CustomEvent("vox-dismiss", { bubbles: !0, composed: !0 })
    );
  }
  render() {
    return l`
      <div class="alert ${this.variant}" role="alert">
        <svg
          class="icon"
          viewBox="0 0 48 48"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
        >
          ${V[es[this.variant]]}
        </svg>
        <div class="body">
          ${this.heading ? l`<p class="heading">${this.heading}</p>` : x}
          <slot></slot>
        </div>
        ${this.dismissible ? l`
              <button class="close" aria-label="Dismiss" @click=${this.dismiss}>
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" aria-hidden="true">
                  <path d="M18 6 6 18M6 6l12 12" />
                </svg>
              </button>
            ` : x}
      </div>
    `;
  }
};
B.styles = d`
    :host {
      display: block;
      font-family: var(--vox-font-family-base);
    }

    :host(:not([open])) {
      display: none;
    }

    .alert {
      display: flex;
      gap: var(--vox-space-3);
      align-items: flex-start;
      padding: var(--vox-space-4);
      border-radius: var(--vox-radius-md);
      border-left: 4px solid;
      font-size: 14px;
      line-height: 1.6;
      color: var(--vox-color-text-1);
    }

    .body {
      flex: 1 1 auto;
    }

    .icon {
      flex: none;
      width: 20px;
      height: 20px;
      margin-top: 1px;
    }

    .heading {
      margin: 0 0 var(--vox-space-1);
      font-size: 14px;
      font-weight: 600;
    }

    .info {
      background-color: var(--vox-color-brand-soft);
      border-color: var(--vox-color-brand-1);
    }
    .info .heading {
      color: var(--vox-color-brand-1);
    }
    .info .icon {
      color: var(--vox-color-brand-1);
    }

    .success {
      background-color: var(--vox-color-tip-soft);
      border-color: var(--vox-color-tip-1);
    }
    .success .heading {
      color: var(--vox-color-tip-1);
    }
    .success .icon {
      color: var(--vox-color-tip-1);
    }

    .warning {
      background-color: var(--vox-color-warning-soft);
      border-color: var(--vox-color-warning-1);
    }
    .warning .heading {
      color: var(--vox-color-warning-1);
    }
    .warning .icon {
      color: var(--vox-color-warning-1);
    }

    .danger {
      background-color: var(--vox-color-danger-soft);
      border-color: var(--vox-color-danger-1);
    }
    .danger .heading {
      color: var(--vox-color-danger-1);
    }
    .danger .icon {
      color: var(--vox-color-danger-1);
    }

    .close {
      flex: none;
      display: flex;
      align-items: center;
      justify-content: center;
      width: 24px;
      height: 24px;
      padding: 0;
      background: none;
      border: none;
      border-radius: var(--vox-radius-sm);
      color: var(--vox-color-text-2);
      cursor: pointer;
    }

    .close:hover {
      color: var(--vox-color-text-1);
    }

    .close:focus-visible {
      outline: 2px solid var(--vox-color-brand-1);
    }

    .close svg {
      width: 14px;
      height: 14px;
    }

    ::slotted(p:first-child) {
      margin-top: 0;
    }

    ::slotted(p:last-child) {
      margin-bottom: 0;
    }
  `;
Re([
  n()
], B.prototype, "variant", 2);
Re([
  n()
], B.prototype, "heading", 2);
Re([
  n({ type: Boolean })
], B.prototype, "dismissible", 2);
Re([
  n({ type: Boolean, reflect: !0 })
], B.prototype, "open", 2);
B = Re([
  h("vox-alert")
], B);
var ts = Object.defineProperty, os = Object.getOwnPropertyDescriptor, $o = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? os(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && ts(e, o, t), t;
};
let Qe = class extends p {
  constructor() {
    super(...arguments), this.variant = "brand";
  }
  render() {
    return l`<span class="badge ${this.variant}"><slot></slot></span>`;
  }
};
Qe.styles = d`
    :host {
      display: inline-block;
    }

    .badge {
      display: inline-flex;
      align-items: center;
      padding: 2px 10px;
      border-radius: var(--vox-radius-full);
      font-family: var(--vox-font-family-base);
      font-size: 12px;
      font-weight: 600;
      line-height: 1.6;
      white-space: nowrap;
    }

    .brand {
      background-color: var(--vox-color-brand-soft);
      color: var(--vox-color-brand-1);
    }

    .tip {
      background-color: var(--vox-color-tip-soft);
      color: var(--vox-color-tip-1);
    }

    .warning {
      background-color: var(--vox-color-warning-soft);
      color: var(--vox-color-warning-1);
    }

    .danger {
      background-color: var(--vox-color-danger-soft);
      color: var(--vox-color-danger-1);
    }

    .neutral {
      background-color: var(--vox-color-bg-soft);
      color: var(--vox-color-text-2);
    }
  `;
$o([
  n()
], Qe.prototype, "variant", 2);
Qe = $o([
  h("vox-badge")
], Qe);
var rs = Object.defineProperty, ss = Object.getOwnPropertyDescriptor, Rt = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? ss(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && rs(e, o, t), t;
};
let Ce = class extends p {
  constructor() {
    super(...arguments), this.date = "";
  }
  render() {
    const r = /* @__PURE__ */ new Date(`${this.date}T00:00:00`), e = !Number.isNaN(r.getTime()), o = e ? r.toLocaleString(this.locale, { month: "short" }) : "—", s = e ? r.getDate() : "–";
    return l`
      <time class="tile" datetime=${this.date}>
        <span class="month">${o}</span>
        <span class="day">${s}</span>
      </time>
    `;
  }
};
Ce.styles = d`
    :host {
      display: inline-block;
      font-family: var(--vox-font-family-base);
    }

    .tile {
      display: flex;
      flex-direction: column;
      align-items: center;
      min-width: 56px;
      border: 1px solid var(--vox-color-divider);
      border-radius: var(--vox-radius-md);
      overflow: hidden;
      background-color: var(--vox-color-bg-elv);
    }

    .month {
      align-self: stretch;
      padding: 2px var(--vox-space-2);
      background-color: var(--vox-color-brand-3);
      color: var(--vox-color-text-inverse);
      font-size: 11px;
      font-weight: 700;
      letter-spacing: 0.08em;
      text-transform: uppercase;
      text-align: center;
    }

    .day {
      padding: var(--vox-space-1) var(--vox-space-2);
      font-size: 24px;
      font-weight: 700;
      color: var(--vox-color-text-1);
    }
  `;
Rt([
  n()
], Ce.prototype, "date", 2);
Rt([
  n()
], Ce.prototype, "locale", 2);
Ce = Rt([
  h("vox-calendar-tile")
], Ce);
var as = Object.defineProperty, is = Object.getOwnPropertyDescriptor, It = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? is(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && as(e, o, t), t;
};
const ns = {
  info: "Info",
  tip: "Tip",
  warning: "Warning",
  danger: "Danger"
}, ls = {
  info: "info",
  tip: "check-circle",
  warning: "warning",
  danger: "x-circle"
};
let Oe = class extends p {
  constructor() {
    super(...arguments), this.variant = "info";
  }
  render() {
    return l`
      <div class="callout ${this.variant}" role="note">
        <svg
          class="icon"
          viewBox="0 0 48 48"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
        >
          ${V[ls[this.variant]]}
        </svg>
        <div class="content">
          <p class="heading">${this.heading ?? ns[this.variant]}</p>
          <slot></slot>
        </div>
      </div>
    `;
  }
};
Oe.styles = d`
    :host {
      display: block;
    }

    .callout {
      display: flex;
      gap: var(--vox-space-3);
      align-items: flex-start;
      border-radius: var(--vox-radius-md);
      padding: var(--vox-space-4);
      font-family: var(--vox-font-family-base);
      font-size: 14px;
      line-height: 1.7;
      color: var(--vox-color-text-2);
    }

    .icon {
      flex: none;
      width: 18px;
      height: 18px;
      margin-top: 2px;
    }

    .content {
      flex: 1 1 auto;
      min-width: 0;
    }

    .heading {
      margin: 0 0 var(--vox-space-2);
      font-size: 14px;
      font-weight: 600;
    }

    .info {
      background-color: var(--vox-color-bg-soft);
    }
    .info .heading {
      color: var(--vox-color-text-1);
    }
    .info .icon {
      color: var(--vox-color-text-1);
    }

    .tip {
      background-color: var(--vox-color-tip-soft);
    }
    .tip .heading {
      color: var(--vox-color-tip-1);
    }
    .tip .icon {
      color: var(--vox-color-tip-1);
    }

    .warning {
      background-color: var(--vox-color-warning-soft);
    }
    .warning .heading {
      color: var(--vox-color-warning-1);
    }
    .warning .icon {
      color: var(--vox-color-warning-1);
    }

    .danger {
      background-color: var(--vox-color-danger-soft);
    }
    .danger .heading {
      color: var(--vox-color-danger-1);
    }
    .danger .icon {
      color: var(--vox-color-danger-1);
    }

    ::slotted(p:first-child) {
      margin-top: 0;
    }

    ::slotted(p:last-child) {
      margin-bottom: 0;
    }
  `;
It([
  n()
], Oe.prototype, "variant", 2);
It([
  n()
], Oe.prototype, "heading", 2);
Oe = It([
  h("vox-callout")
], Oe);
var cs = Object.defineProperty, ds = Object.getOwnPropertyDescriptor, vt = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? ds(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && cs(e, o, t), t;
};
let Q = class extends p {
  constructor() {
    super(...arguments), this.heading = "", this.hasIcon = !1, this.hasBadge = !1, this.hasFooter = !1;
  }
  handleIconSlotChange(r) {
    const e = r.target;
    this.hasIcon = e.assignedNodes({ flatten: !0 }).length > 0, this.requestUpdate();
  }
  handleBadgeSlotChange(r) {
    const e = r.target;
    this.hasBadge = e.assignedNodes({ flatten: !0 }).length > 0, this.requestUpdate();
  }
  handleFooterSlotChange(r) {
    const e = r.target;
    this.hasFooter = e.assignedNodes({ flatten: !0 }).length > 0, this.requestUpdate();
  }
  render() {
    const r = l`
      <div class="badge ${this.hasBadge ? "has-badge" : ""}">
        <slot name="badge" @slotchange=${this.handleBadgeSlotChange}></slot>
      </div>
      <div class="icon ${this.hasIcon ? "has-icon" : ""}">
        <slot name="icon" @slotchange=${this.handleIconSlotChange}></slot>
      </div>
      <h3 class="heading">${this.heading}</h3>
      <div class="body"><slot></slot></div>
      <div class="footer ${this.hasFooter ? "has-footer" : ""}">
        <slot name="footer" @slotchange=${this.handleFooterSlotChange}></slot>
      </div>
    `;
    return this.href !== void 0 ? l`<a class="card" href=${this.href} target=${this.target ?? x}>${r}</a>` : l`<div class="card">${r}</div>`;
  }
};
Q.styles = d`
    :host {
      display: block;
    }

    .card {
      position: relative;
      display: flex;
      flex-direction: column;
      height: 100%;
      box-sizing: border-box;
      background-color: var(--vox-color-bg-soft);
      border: 1px solid var(--vox-color-bg-soft);
      border-radius: var(--vox-radius-lg);
      padding: var(--vox-space-6);
      font-family: var(--vox-font-family-base);
      /* Slotted content (e.g. an icon using stroke="currentColor")
         inherits from this element's position in the flat tree — when
         rendered as an <a>, that means the browser's default link color
         without this, since nothing else in .card sets one. */
      color: var(--vox-color-text-1);
      text-decoration: none;
      transition: border-color var(--vox-transition-base);
    }

    .badge {
      position: absolute;
      top: var(--vox-space-6);
      right: var(--vox-space-6);
    }

    .badge:not(.has-badge) {
      display: none;
    }

    a.card:hover,
    a.card:focus-visible {
      border-color: var(--vox-color-brand-1);
    }

    a.card:focus-visible {
      outline: 2px solid var(--vox-color-brand-1);
      outline-offset: 2px;
    }

    .icon {
      display: flex;
      align-items: center;
      justify-content: center;
      width: 48px;
      height: 48px;
      margin-bottom: var(--vox-space-4);
      background-color: var(--vox-color-bg-elv);
      border-radius: var(--vox-radius-md);
      font-size: 24px;
    }

    .icon:not(.has-icon) {
      display: none;
    }

    .heading {
      margin: 0;
      font-size: 16px;
      font-weight: 600;
      line-height: 1.5;
      color: var(--vox-color-text-1);
    }

    .body {
      margin-top: var(--vox-space-2);
      font-size: 14px;
      line-height: 1.6;
      color: var(--vox-color-text-2);
    }

    .footer {
      margin-top: auto;
      padding-top: var(--vox-space-4);
    }

    .footer:not(.has-footer) {
      display: none;
    }
  `;
vt([
  n()
], Q.prototype, "heading", 2);
vt([
  n()
], Q.prototype, "href", 2);
vt([
  n()
], Q.prototype, "target", 2);
Q = vt([
  h("vox-card")
], Q);
var hs = Object.defineProperty, ps = Object.getOwnPropertyDescriptor, wo = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? ps(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && hs(e, o, t), t;
};
let et = class extends p {
  constructor() {
    super(...arguments), this.heading = "", this.hasBody = !1, this.hasActions = !1;
  }
  handleBodySlotChange(r) {
    const e = r.target;
    this.hasBody = e.assignedNodes({ flatten: !0 }).length > 0, this.requestUpdate();
  }
  handleActionsSlotChange(r) {
    const e = r.target;
    this.hasActions = e.assignedNodes({ flatten: !0 }).length > 0, this.requestUpdate();
  }
  render() {
    return l`
      <div class="band">
        ${this.heading ? l`<h2 class="heading">${this.heading}</h2>` : x}
        <div class="body ${this.hasBody ? "has-content" : ""}">
          <slot @slotchange=${this.handleBodySlotChange}></slot>
        </div>
        <div class="actions ${this.hasActions ? "has-content" : ""}">
          <slot name="actions" @slotchange=${this.handleActionsSlotChange}></slot>
        </div>
      </div>
    `;
  }
};
et.styles = d`
    :host {
      display: block;
      font-family: var(--vox-font-family-base);
    }

    .band {
      text-align: center;
      padding: var(--vox-space-8);
      background-color: var(--vox-color-bg-soft);
      border-radius: var(--vox-radius-lg);
    }

    .heading {
      margin: 0;
      font-family: var(--vox-font-family-display);
      font-size: 28px;
      font-weight: 600;
      line-height: 1.3;
      color: var(--vox-color-text-1);
    }

    .body {
      max-width: 36rem;
      margin: var(--vox-space-3) auto 0;
      font-size: 16px;
      line-height: 1.6;
      color: var(--vox-color-text-2);
    }

    .body:not(.has-content) {
      display: none;
    }

    .body ::slotted(a) {
      color: var(--vox-color-brand-1);
      text-decoration: underline;
    }

    .body ::slotted(a:hover) {
      color: var(--vox-color-brand-2);
    }

    .actions {
      display: flex;
      gap: var(--vox-space-3);
      justify-content: center;
      flex-wrap: wrap;
      margin-top: var(--vox-space-6);
    }

    .actions:not(.has-content) {
      display: none;
    }
  `;
wo([
  n()
], et.prototype, "heading", 2);
et = wo([
  h("vox-cta-band")
], et);
var vs = Object.defineProperty, us = Object.getOwnPropertyDescriptor, _o = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? us(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && vs(e, o, t), t;
};
let tt = class extends p {
  constructor() {
    super(...arguments), this.name = "", this.hasIcon = !1;
  }
  handleIconSlotChange(r) {
    const e = r.target;
    this.hasIcon = e.assignedNodes({ flatten: !0 }).length > 0, this.requestUpdate();
  }
  render() {
    return l`
      <span class="icon ${this.hasIcon ? "has-icon" : ""}">
        <slot name="icon" @slotchange=${this.handleIconSlotChange}></slot>
      </span>
      ${this.name ? l`<span class="sr-only">${this.name}: </span>` : x}
      <slot></slot>
    `;
  }
};
tt.styles = d`
    :host {
      display: inline-flex;
      align-items: center;
      gap: var(--vox-space-1);
      font-family: var(--vox-font-family-base);
      font-size: 13px;
      color: var(--vox-color-text-2);
    }

    .icon {
      display: flex;
      flex: none;
      color: var(--vox-color-text-3);
    }

    .icon:not(.has-icon) {
      display: none;
    }

    .sr-only {
      position: absolute;
      width: 1px;
      height: 1px;
      padding: 0;
      margin: -1px;
      overflow: hidden;
      clip: rect(0, 0, 0, 0);
      white-space: nowrap;
      border: 0;
    }
  `;
_o([
  n()
], tt.prototype, "name", 2);
tt = _o([
  h("vox-datum")
], tt);
var xs = Object.defineProperty, fs = Object.getOwnPropertyDescriptor, Co = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? fs(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && xs(e, o, t), t;
};
let ot = class extends p {
  constructor() {
    super(...arguments), this.heading = "";
  }
  render() {
    return l`
      <div class="empty">
        <div class="icon"><slot name="icon"></slot></div>
        <h3 class="heading">${this.heading}</h3>
        <div class="body"><slot></slot></div>
        <div class="actions"><slot name="actions"></slot></div>
      </div>
    `;
  }
};
ot.styles = d`
    :host {
      display: block;
      font-family: var(--vox-font-family-base);
    }

    .empty {
      display: flex;
      flex-direction: column;
      align-items: center;
      text-align: center;
      padding: var(--vox-space-8);
      border: 1px dashed var(--vox-color-border);
      border-radius: var(--vox-radius-lg);
    }

    .icon {
      font-size: 32px;
      margin-bottom: var(--vox-space-3);
    }

    .heading {
      margin: 0;
      font-size: 18px;
      font-weight: 600;
      color: var(--vox-color-text-1);
    }

    .body {
      max-width: 32rem;
      margin-top: var(--vox-space-2);
      font-size: 14px;
      line-height: 1.6;
      color: var(--vox-color-text-2);
    }

    .actions {
      display: flex;
      gap: var(--vox-space-3);
      flex-wrap: wrap;
      justify-content: center;
      margin-top: var(--vox-space-4);
    }
  `;
Co([
  n()
], ot.prototype, "heading", 2);
ot = Co([
  h("vox-empty-state")
], ot);
var gs = Object.defineProperty, bs = Object.getOwnPropertyDescriptor, Ut = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? bs(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && gs(e, o, t), t;
};
let Ct = class extends p {
  render() {
    return l`
      <footer>
        <div class="inner">
          <div class="columns"><slot></slot></div>
          <div class="bottom"><slot name="bottom"></slot></div>
        </div>
      </footer>
    `;
  }
};
Ct.styles = d`
    :host {
      display: block;
      font-family: var(--vox-font-family-base);
      background-color: var(--vox-color-bg-alt);
      border-top: 1px solid var(--vox-color-divider);
    }

    .inner {
      max-width: 1280px;
      margin: 0 auto;
      padding: var(--vox-space-8) var(--vox-space-6);
    }

    .columns {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
      gap: var(--vox-space-6);
    }

    .bottom {
      margin-top: var(--vox-space-8);
      padding-top: var(--vox-space-4);
      border-top: 1px solid var(--vox-color-divider);
      font-size: 13px;
      color: var(--vox-color-text-3);
    }
  `;
Ct = Ut([
  h("vox-footer")
], Ct);
let rt = class extends p {
  constructor() {
    super(...arguments), this.heading = "";
  }
  render() {
    return l`
      <h3 class="heading">${this.heading}</h3>
      <div class="links"><slot></slot></div>
    `;
  }
};
rt.styles = d`
    :host {
      display: block;
      font-family: var(--vox-font-family-base);
    }

    .heading {
      margin: 0 0 var(--vox-space-3);
      font-size: 13px;
      font-weight: 600;
      letter-spacing: 0.05em;
      text-transform: uppercase;
      color: var(--vox-color-text-1);
    }

    .links {
      display: flex;
      flex-direction: column;
      gap: var(--vox-space-2);
    }

    ::slotted(a) {
      color: var(--vox-color-text-2);
      font-size: 14px;
      text-decoration: none;
    }

    ::slotted(a:hover) {
      color: var(--vox-color-brand-1);
      text-decoration: underline;
    }
  `;
Ut([
  n()
], rt.prototype, "heading", 2);
rt = Ut([
  h("vox-footer-column")
], rt);
var ms = Object.defineProperty, ys = Object.getOwnPropertyDescriptor, ut = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? ys(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && ms(e, o, t), t;
};
let ee = class extends p {
  constructor() {
    super(...arguments), this.cols = 0, this.min = "240px", this.gap = "md";
  }
  updated() {
    this.style.gridTemplateColumns = this.cols > 0 ? `repeat(${this.cols}, minmax(0, 1fr))` : `repeat(auto-fit, minmax(min(${this.min}, 100%), 1fr))`;
  }
  render() {
    return l`<slot></slot>`;
  }
};
ee.styles = d`
    :host {
      display: grid;
    }

    :host([gap='sm']) {
      gap: var(--vox-space-2);
    }

    :host([gap='md']) {
      gap: var(--vox-space-4);
    }

    :host([gap='lg']) {
      gap: var(--vox-space-6);
    }
  `;
ut([
  n({ type: Number })
], ee.prototype, "cols", 2);
ut([
  n()
], ee.prototype, "min", 2);
ut([
  n({ reflect: !0 })
], ee.prototype, "gap", 2);
ee = ut([
  h("vox-grid")
], ee);
var $s = Object.defineProperty, ws = Object.getOwnPropertyDescriptor, qt = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? ws(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && $s(e, o, t), t;
};
let Me = class extends p {
  constructor() {
    super(...arguments), this.eyebrow = "", this.heading = "", this.hasActions = !1;
  }
  handleActionsSlotChange(r) {
    const e = r.target;
    this.hasActions = e.assignedNodes({ flatten: !0 }).length > 0, this.requestUpdate();
  }
  render() {
    return l`
      <header class="hero">
        ${this.eyebrow ? l`<p class="eyebrow">${this.eyebrow}</p>` : x}
        <h1 class="heading">${this.heading}</h1>
        <div class="body"><slot></slot></div>
        <div class="actions ${this.hasActions ? "has-content" : ""}">
          <slot name="actions" @slotchange=${this.handleActionsSlotChange}></slot>
        </div>
      </header>
    `;
  }
};
Me.styles = d`
    :host {
      display: block;
      font-family: var(--vox-font-family-base);
    }

    .hero {
      padding: var(--vox-space-8) 0;
      border-bottom: 1px solid var(--vox-color-divider);
    }

    .eyebrow {
      margin: 0 0 var(--vox-space-2);
      font-size: 13px;
      font-weight: 600;
      letter-spacing: 0.05em;
      text-transform: uppercase;
      color: var(--vox-color-brand-1);
    }

    .heading {
      margin: 0;
      font-family: var(--vox-font-family-display);
      font-size: 36px;
      font-weight: 600;
      line-height: 1.25;
      color: var(--vox-color-text-1);
    }

    .body {
      max-width: 44rem;
      margin-top: var(--vox-space-3);
      font-size: 17px;
      line-height: 1.7;
      color: var(--vox-color-text-2);
    }

    .body ::slotted(a) {
      color: var(--vox-color-brand-1);
      text-decoration: underline;
    }

    .body ::slotted(a:hover) {
      color: var(--vox-color-brand-2);
    }

    .actions {
      display: flex;
      gap: var(--vox-space-3);
      flex-wrap: wrap;
      margin-top: var(--vox-space-6);
    }

    .actions:not(.has-content) {
      display: none;
    }
  `;
qt([
  n()
], Me.prototype, "eyebrow", 2);
qt([
  n()
], Me.prototype, "heading", 2);
Me = qt([
  h("vox-hero")
], Me);
var _s = Object.defineProperty, Cs = Object.getOwnPropertyDescriptor, xt = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? Cs(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && _s(e, o, t), t;
};
let Ot = class extends p {
  render() {
    return l`<slot></slot>`;
  }
};
Ot.styles = d`
    :host {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(min(260px, 100%), 1fr));
      gap: var(--vox-space-4);
    }
  `;
Ot = xt([
  h("vox-link-hub")
], Ot);
let Pe = class extends p {
  constructor() {
    super(...arguments), this.href = "#", this.heading = "", this.hasIcon = !1;
  }
  handleIconSlotChange(r) {
    const e = r.target;
    this.hasIcon = e.assignedNodes({ flatten: !0 }).length > 0, this.requestUpdate();
  }
  render() {
    return l`
      <a href=${this.href}>
        <span class="heading">
          <span class="icon ${this.hasIcon ? "has-icon" : ""}">
            <slot name="icon" @slotchange=${this.handleIconSlotChange}></slot>
          </span>
          ${this.heading}
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d="M5 12h14" />
            <path d="m13 6 6 6-6 6" />
          </svg>
        </span>
        <span class="body"><slot></slot></span>
      </a>
    `;
  }
};
Pe.styles = d`
    :host {
      display: block;
      font-family: var(--vox-font-family-base);
    }

    .icon {
      display: flex;
      flex: none;
    }

    .icon:not(.has-icon) {
      display: none;
    }

    a {
      display: block;
      height: 100%;
      box-sizing: border-box;
      padding: var(--vox-space-4);
      border-bottom: 2px solid var(--vox-color-divider);
      text-decoration: none;
      transition: border-color var(--vox-transition-base);
    }

    a:hover,
    a:focus-visible {
      border-bottom-color: var(--vox-color-brand-1);
    }

    a:focus-visible {
      outline: 2px solid var(--vox-color-brand-1);
      outline-offset: 2px;
    }

    .heading {
      display: flex;
      align-items: center;
      gap: var(--vox-space-2);
      font-size: 16px;
      font-weight: 600;
      color: var(--vox-color-brand-1);
    }

    .heading svg {
      width: 14px;
      height: 14px;
      transition: transform var(--vox-transition-fast);
    }

    a:hover .heading svg {
      transform: translateX(3px);
    }

    .body {
      margin-top: var(--vox-space-1);
      font-size: 14px;
      line-height: 1.6;
      color: var(--vox-color-text-2);
    }
  `;
xt([
  n()
], Pe.prototype, "href", 2);
xt([
  n()
], Pe.prototype, "heading", 2);
Pe = xt([
  h("vox-link-hub-item")
], Pe);
var Os = Object.defineProperty, Ms = Object.getOwnPropertyDescriptor, Ft = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? Ms(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && Os(e, o, t), t;
};
let Ae = class extends p {
  constructor() {
    super(...arguments), this.size = "md", this.label = "Loading";
  }
  render() {
    return l`
      <div role="status">
        <div class="spinner"></div>
        <span class="visually-hidden">${this.label}</span>
      </div>
    `;
  }
};
Ae.styles = d`
    :host {
      display: inline-block;
    }

    .spinner {
      box-sizing: border-box;
      border-radius: 50%;
      border-style: solid;
      border-color: var(--vox-color-brand-soft);
      border-top-color: var(--vox-color-brand-3);
      animation: spin 0.8s linear infinite;
    }

    :host([size='sm']) .spinner {
      width: 16px;
      height: 16px;
      border-width: 2px;
    }

    :host([size='md']) .spinner {
      width: 28px;
      height: 28px;
      border-width: 3px;
    }

    :host([size='lg']) .spinner {
      width: 44px;
      height: 44px;
      border-width: 4px;
    }

    @keyframes spin {
      to {
        transform: rotate(360deg);
      }
    }

    @media (prefers-reduced-motion: reduce) {
      .spinner {
        animation-duration: 2.4s;
      }
    }

    .visually-hidden {
      position: absolute;
      width: 1px;
      height: 1px;
      overflow: hidden;
      clip: rect(0 0 0 0);
      white-space: nowrap;
    }
  `;
Ft([
  n({ reflect: !0 })
], Ae.prototype, "size", 2);
Ft([
  n()
], Ae.prototype, "label", 2);
Ae = Ft([
  h("vox-loader")
], Ae);
var Ps = Object.defineProperty, As = Object.getOwnPropertyDescriptor, Oo = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? As(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && Ps(e, o, t), t;
};
let st = class extends p {
  constructor() {
    super(...arguments), this.label = "Pagination";
  }
  render() {
    return l`
      <nav aria-label=${this.label}>
        <slot></slot>
      </nav>
    `;
  }
};
st.styles = d`
    :host {
      display: block;
      font-family: var(--vox-font-family-base);
    }

    nav {
      display: flex;
      gap: var(--vox-space-1);
      flex-wrap: wrap;
    }

    ::slotted(a) {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      min-width: 34px;
      height: 34px;
      padding: 0 var(--vox-space-2);
      border-radius: var(--vox-radius-md);
      color: var(--vox-color-text-2);
      font-size: 14px;
      font-weight: 500;
      text-decoration: none;
      transition:
        color var(--vox-transition-fast),
        background-color var(--vox-transition-fast);
    }

    ::slotted(a:hover) {
      color: var(--vox-color-brand-1);
      background-color: var(--vox-color-brand-soft);
    }

    ::slotted(a[aria-current='page']) {
      background-color: var(--vox-color-brand-3);
      color: var(--vox-color-text-inverse);
      font-weight: 600;
    }
  `;
Oo([
  n()
], st.prototype, "label", 2);
st = Oo([
  h("vox-pagination")
], st);
var ks = Object.defineProperty, Ls = Object.getOwnPropertyDescriptor, Zt = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? Ls(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && ks(e, o, t), t;
};
let ke = class extends p {
  constructor() {
    super(...arguments), this.attribution = "", this.detail = "";
  }
  render() {
    return l`
      <blockquote>
        <div class="text"><slot></slot></div>
        ${this.attribution ? l`
              <footer>
                <span class="attribution">${this.attribution}</span>
                ${this.detail ? l`<span class="detail"> — ${this.detail}</span>` : x}
              </footer>
            ` : x}
      </blockquote>
    `;
  }
};
ke.styles = d`
    :host {
      display: block;
      font-family: var(--vox-font-family-base);
    }

    blockquote {
      margin: 0;
      padding-left: var(--vox-space-6);
      border-left: 4px solid var(--vox-color-brand-3);
    }

    .text {
      font-family: var(--vox-font-family-display);
      font-style: italic;
      font-size: 20px;
      line-height: 1.6;
      color: var(--vox-color-text-1);
    }

    .text::before {
      content: '“';
    }

    .text::after {
      content: '”';
    }

    footer {
      margin-top: var(--vox-space-3);
      font-size: 14px;
    }

    .attribution {
      font-weight: 600;
      color: var(--vox-color-text-1);
    }

    .detail {
      color: var(--vox-color-text-2);
    }
  `;
Zt([
  n()
], ke.prototype, "attribution", 2);
Zt([
  n()
], ke.prototype, "detail", 2);
ke = Zt([
  h("vox-quote")
], ke);
var Es = Object.defineProperty, Ss = Object.getOwnPropertyDescriptor, Ie = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? Ss(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && Es(e, o, t), t;
};
let Mt = class extends p {
  render() {
    return l`<slot></slot>`;
  }
};
Mt.styles = d`
    :host {
      display: grid;
      grid-template-columns: repeat(3, max-content) 1fr;
      column-gap: var(--vox-space-4);
    }
  `;
Mt = Ie([
  h("vox-record-list")
], Mt);
let te = class extends p {
  constructor() {
    super(...arguments), this.heading = "", this.size = "md", this.hasMeta = !1, this.hasEnd = !1;
  }
  handleMetaSlotChange(r) {
    const e = r.target;
    this.hasMeta = e.assignedNodes({ flatten: !0 }).length > 0, this.requestUpdate();
  }
  handleEndSlotChange(r) {
    const e = r.target;
    this.hasEnd = e.assignedNodes({ flatten: !0 }).length > 0, this.requestUpdate();
  }
  render() {
    if (this.size === "sm") {
      const r = l`
        <span class="meta ${this.hasMeta ? "has-meta" : ""}">
          <slot @slotchange=${this.handleMetaSlotChange}></slot>
        </span>
      `;
      return l`
        ${this.href ? l`<a class="cell" href=${this.href}>${this.heading}</a>` : l`<span class="cell">${this.heading}</span>`}
        ${this.hasMeta && this.href ? l`<a class="cell" href=${this.href}>${r}</a>` : l`<span class="cell">${r}</span>`}
        <span class="cell">
          <slot name="end" @slotchange=${this.handleEndSlotChange}></slot>
        </span>
      `;
    }
    return l`
      <div class="row">
        <div class="main">
          ${this.href ? l`<a class="heading" href=${this.href}>${this.heading}</a>` : l`<span class="heading">${this.heading}</span>`}
          <div class="meta ${this.hasMeta ? "has-meta" : ""}">
            <slot @slotchange=${this.handleMetaSlotChange}></slot>
          </div>
        </div>
        <span class="end ${this.hasEnd ? "has-end" : ""}">
          <slot name="end" @slotchange=${this.handleEndSlotChange}></slot>
        </span>
        ${this.href ? l`
              <a class="arrow" href=${this.href} aria-label="View ${this.heading}">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                  <path d="M5 12h14" />
                  <path d="m13 6 6 6-6 6" />
                </svg>
              </a>
            ` : x}
      </div>
    `;
  }
};
te.styles = d`
    :host {
      display: block;
      grid-column: 1 / -1;
      font-family: var(--vox-font-family-base);
    }

    .row {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: var(--vox-space-4);
      padding: var(--vox-space-4) 0;
      border-bottom: 1px solid var(--vox-color-divider);
    }

    :host(:last-child) .row {
      border-bottom: none;
    }

    .main {
      display: flex;
      flex-direction: column;
      gap: var(--vox-space-1);
      min-width: 0;
    }

    .heading {
      font-size: 16px;
      font-weight: 600;
      line-height: 1.4;
    }

    a.heading {
      color: var(--vox-color-brand-1);
      text-decoration: none;
    }

    a.heading:hover,
    a.heading:focus-visible {
      text-decoration: underline;
    }

    a.heading:focus-visible {
      outline: 2px solid var(--vox-color-brand-1);
      outline-offset: 2px;
    }

    span.heading {
      color: var(--vox-color-text-1);
    }

    .meta {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: var(--vox-space-4);
    }

    .meta:not(.has-meta) {
      display: none;
    }

    .arrow {
      display: flex;
      flex: none;
      align-items: center;
      justify-content: center;
      width: 36px;
      height: 36px;
      border-radius: var(--vox-radius-full);
      background-color: var(--vox-color-bg-soft);
      color: var(--vox-color-brand-1);
      text-decoration: none;
      transition: background-color var(--vox-transition-fast);
    }

    .arrow:hover {
      background-color: var(--vox-color-brand-soft);
    }

    .arrow:focus-visible {
      outline: 2px solid var(--vox-color-brand-1);
      outline-offset: 2px;
    }

    .arrow svg {
      width: 16px;
      height: 16px;
    }

    .end:not(.has-end) {
      display: none;
    }

    /* Small size: heading, meta, and end each become their own subgrid
       column, aligned against the same column in every other size="sm"
       row in the parent <vox-record-list> — the arrow drops out since
       there's no room for it at this density, and the whole "row" is
       really 3 separate grid cells rather than one box, so heading and
       the meta cell each link to href individually. */
    :host([size='sm']) {
      display: grid;
      grid-template-columns: subgrid;
      grid-column: 1 / -1;
      align-items: baseline;
      column-gap: var(--vox-space-4);
      padding: var(--vox-space-2) 0;
      border-bottom: 1px solid var(--vox-color-divider);
      font-size: 13px;
    }

    :host([size='sm']:last-child) {
      border-bottom: none;
    }

    :host([size='sm']) .meta {
      flex-wrap: nowrap;
      gap: var(--vox-space-2);
    }

    .cell {
      color: var(--vox-color-text-2);
      text-decoration: none;
    }

    a.cell:hover,
    a.cell:focus-visible {
      text-decoration: underline;
    }

    a.cell:focus-visible {
      outline: 2px solid var(--vox-color-brand-1);
      outline-offset: 2px;
    }
  `;
Ie([
  n()
], te.prototype, "heading", 2);
Ie([
  n()
], te.prototype, "href", 2);
Ie([
  n({ reflect: !0 })
], te.prototype, "size", 2);
te = Ie([
  h("vox-record-list-item")
], te);
var js = Object.defineProperty, Vs = Object.getOwnPropertyDescriptor, U = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? Vs(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && js(e, o, t), t;
};
let at = class extends p {
  constructor() {
    super(...arguments), this.heading = "", this.hasDescription = !1;
  }
  handleDescriptionSlotChange(r) {
    const e = r.target;
    this.hasDescription = e.assignedNodes({ flatten: !0 }).length > 0, this.requestUpdate();
  }
  render() {
    return l`
      <h2 class="heading">${this.heading}</h2>
      <div class="description ${this.hasDescription ? "has-content" : ""}">
        <slot
          name="description"
          @slotchange=${this.handleDescriptionSlotChange}
        ></slot>
      </div>
      <div class="grid">
        <slot></slot>
      </div>
    `;
  }
};
at.styles = d`
    :host {
      display: block;
      font-family: var(--vox-font-family-base);
    }

    .heading {
      margin: 0;
      font-size: 22px;
      font-weight: 700;
      line-height: 1.3;
      color: var(--vox-color-text-1);
    }

    .description {
      margin: var(--vox-space-2) 0 0;
      font-size: 15px;
      line-height: 1.7;
      color: var(--vox-color-text-2);
    }

    .description:not(.has-content) {
      display: none;
    }

    .grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(min(220px, 100%), 1fr));
      gap: var(--vox-space-4);
      margin-top: var(--vox-space-6);
    }
  `;
U([
  n()
], at.prototype, "heading", 2);
at = U([
  h("vox-sponsor-tier")
], at);
let N = class extends p {
  constructor() {
    super(...arguments), this.name = "", this.hasBody = !1;
  }
  handleBodySlotChange(r) {
    const e = r.target;
    this.hasBody = e.assignedNodes({ flatten: !0 }).length > 0, this.requestUpdate();
  }
  render() {
    const r = l`
      <div class="logo ${this.logo ? "has-logo" : ""}">
        ${this.logo ? l`<img src=${this.logo} alt=${this.name} />` : x}
      </div>
      <span class="name">${this.name}</span>
      <div class="body ${this.hasBody ? "has-content" : ""}">
        <slot @slotchange=${this.handleBodySlotChange}></slot>
      </div>
    `;
    return this.href !== void 0 ? l`<a
          class="sponsor"
          href=${this.href}
          target=${this.target ?? x}
          >${r}</a
        >` : l`<div class="sponsor">${r}</div>`;
  }
};
N.styles = d`
    :host {
      display: block;
    }

    .sponsor {
      display: flex;
      flex-direction: column;
      height: 100%;
      box-sizing: border-box;
      gap: var(--vox-space-3);
      padding: var(--vox-space-4);
      background-color: var(--vox-color-bg-soft);
      border: 1px solid var(--vox-color-bg-soft);
      border-radius: var(--vox-radius-lg);
      font-family: var(--vox-font-family-base);
      text-decoration: none;
      transition: border-color var(--vox-transition-base);
    }

    a.sponsor:hover,
    a.sponsor:focus-visible {
      border-color: var(--vox-color-brand-1);
    }

    a.sponsor:focus-visible {
      outline: 2px solid var(--vox-color-brand-1);
      outline-offset: 2px;
    }

    .logo {
      display: flex;
      align-items: center;
      justify-content: center;
      height: 64px;
      background-color: var(--vox-color-bg-elv);
      border-radius: var(--vox-radius-md);
    }

    .logo:not(.has-logo) {
      display: none;
    }

    .logo img {
      max-width: 80%;
      max-height: 40px;
      object-fit: contain;
    }

    .name {
      font-size: 15px;
      font-weight: 600;
      line-height: 1.4;
      color: var(--vox-color-text-1);
    }

    .body {
      font-size: 13px;
      line-height: 1.6;
      color: var(--vox-color-text-2);
    }

    .body:not(.has-content) {
      display: none;
    }
  `;
U([
  n()
], N.prototype, "name", 2);
U([
  n()
], N.prototype, "href", 2);
U([
  n()
], N.prototype, "logo", 2);
U([
  n()
], N.prototype, "target", 2);
N = U([
  h("vox-sponsor")
], N);
var Ds = Object.defineProperty, zs = Object.getOwnPropertyDescriptor, Kt = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? zs(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && Ds(e, o, t), t;
};
let Le = class extends p {
  constructor() {
    super(...arguments), this.value = "", this.label = "";
  }
  render() {
    return l`
      <div class="value">${this.value}</div>
      <div class="label">${this.label}</div>
      <div class="description"><slot></slot></div>
    `;
  }
};
Le.styles = d`
    :host {
      display: block;
      font-family: var(--vox-font-family-base);
    }

    .value {
      font-size: 40px;
      font-weight: 700;
      line-height: 1.1;
      color: var(--vox-color-brand-1);
    }

    .label {
      margin-top: var(--vox-space-1);
      font-size: 14px;
      font-weight: 600;
      color: var(--vox-color-text-1);
    }

    .description {
      margin-top: var(--vox-space-1);
      font-size: 13px;
      line-height: 1.6;
      color: var(--vox-color-text-2);
    }
  `;
Kt([
  n()
], Le.prototype, "value", 2);
Kt([
  n()
], Le.prototype, "label", 2);
Le = Kt([
  h("vox-stat")
], Le);
var Hs = Object.defineProperty, Ts = Object.getOwnPropertyDescriptor, Ue = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? Ts(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && Hs(e, o, t), t;
};
let Pt = class extends p {
  numberSteps() {
    this.querySelectorAll("vox-step").forEach((r, e) => {
      r.number = e + 1;
    });
  }
  render() {
    return l`
      <div class="steps" role="list" aria-label="Progress">
        <slot @slotchange=${this.numberSteps}></slot>
      </div>
    `;
  }
};
Pt.styles = d`
    :host {
      display: block;
    }

    .steps {
      display: flex;
      align-items: flex-start;
    }

    ::slotted(vox-step) {
      flex: 1 1 0;
    }
  `;
Pt = Ue([
  h("vox-step-indicator")
], Pt);
let oe = class extends p {
  constructor() {
    super(...arguments), this.label = "", this.state = "upcoming", this.number = 1;
  }
  render() {
    return l`
      <div
        class="step"
        role="listitem"
        aria-current=${this.state === "current" ? "step" : "false"}
      >
        <span class="marker">
          ${this.state === "complete" ? l`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                <path d="m4 12 5 5L20 6" />
              </svg>` : this.number}
        </span>
        <span class="label">${this.label}</span>
      </div>
    `;
  }
};
oe.styles = d`
    :host {
      display: block;
      font-family: var(--vox-font-family-base);
    }

    .step {
      position: relative;
      display: flex;
      flex-direction: column;
      align-items: center;
      text-align: center;
      gap: var(--vox-space-2);
      padding: 0 var(--vox-space-2);
    }

    /* Connector line to the previous step. */
    :host(:not(:first-child)) .step::before {
      content: '';
      position: absolute;
      top: 14px;
      right: 50%;
      width: 100%;
      height: 2px;
      background-color: var(--vox-color-divider);
      z-index: 0;
    }

    :host([state='complete']) .step::before {
      background-color: var(--vox-color-brand-3);
    }

    .marker {
      position: relative;
      z-index: 1;
      display: flex;
      align-items: center;
      justify-content: center;
      width: 28px;
      height: 28px;
      border-radius: 50%;
      border: 2px solid var(--vox-color-divider);
      background-color: var(--vox-color-bg);
      color: var(--vox-color-text-2);
      font-size: 13px;
      font-weight: 600;
    }

    :host([state='complete']) .marker {
      background-color: var(--vox-color-brand-3);
      border-color: var(--vox-color-brand-3);
      color: var(--vox-color-text-inverse);
    }

    :host([state='current']) .marker {
      border-color: var(--vox-color-brand-3);
      color: var(--vox-color-brand-1);
    }

    .marker svg {
      width: 14px;
      height: 14px;
    }

    .label {
      font-size: 13px;
      color: var(--vox-color-text-2);
    }

    :host([state='current']) .label {
      font-weight: 600;
      color: var(--vox-color-text-1);
    }
  `;
Ue([
  n()
], oe.prototype, "label", 2);
Ue([
  n({ reflect: !0 })
], oe.prototype, "state", 2);
Ue([
  n({ type: Number })
], oe.prototype, "number", 2);
oe = Ue([
  h("vox-step")
], oe);
var Bs = Object.defineProperty, Ns = Object.getOwnPropertyDescriptor, ft = (r, e, o, s) => {
  for (var t = s > 1 ? void 0 : s ? Ns(e, o) : e, a = r.length - 1, i; a >= 0; a--)
    (i = r[a]) && (t = (s ? i(e, o, t) : i(t)) || t);
  return s && t && Bs(e, o, t), t;
};
let At = class extends p {
  render() {
    return l`<slot></slot>`;
  }
};
At.styles = d`
    :host {
      display: block;
    }
  `;
At = ft([
  h("vox-timeline")
], At);
let Ee = class extends p {
  constructor() {
    super(...arguments), this.heading = "", this.date = "";
  }
  render() {
    return l`
      <div class="item">
        <span class="dot" aria-hidden="true"></span>
        ${this.date ? l`<div class="date">${this.date}</div>` : x}
        <h3 class="heading">${this.heading}</h3>
        <div class="body"><slot></slot></div>
      </div>
    `;
  }
};
Ee.styles = d`
    :host {
      display: block;
      font-family: var(--vox-font-family-base);
    }

    .item {
      position: relative;
      padding: 0 0 var(--vox-space-6) var(--vox-space-6);
      border-left: 2px solid var(--vox-color-divider);
    }

    :host(:last-child) .item {
      border-left-color: transparent;
      padding-bottom: 0;
    }

    .dot {
      position: absolute;
      top: 4px;
      left: -7px;
      width: 12px;
      height: 12px;
      border-radius: 50%;
      background-color: var(--vox-color-brand-3);
      border: 2px solid var(--vox-color-bg);
    }

    .date {
      font-size: 12px;
      font-weight: 600;
      letter-spacing: 0.05em;
      text-transform: uppercase;
      color: var(--vox-color-text-3);
    }

    .heading {
      margin: var(--vox-space-1) 0 0;
      font-size: 16px;
      font-weight: 600;
      color: var(--vox-color-text-1);
    }

    .body {
      margin-top: var(--vox-space-1);
      font-size: 14px;
      line-height: 1.6;
      color: var(--vox-color-text-2);
    }
  `;
ft([
  n()
], Ee.prototype, "heading", 2);
ft([
  n()
], Ee.prototype, "date", 2);
Ee = ft([
  h("vox-timeline-item")
], Ee);
export {
  Ye as VoxAccordion,
  _e as VoxAccordionItem,
  B as VoxAlert,
  z as VoxAvatar,
  Qe as VoxBadge,
  xe as VoxBillboard,
  wt as VoxBreadcrumbs,
  _ as VoxButton,
  Ce as VoxCalendarTile,
  Oe as VoxCallout,
  Q as VoxCard,
  pe as VoxCheckbox,
  he as VoxCta,
  et as VoxCtaBand,
  tt as VoxDatum,
  T as VoxDialog,
  $e as VoxDisclosure,
  we as VoxDropdown,
  ot as VoxEmptyState,
  P as VoxFileInput,
  Ct as VoxFooter,
  rt as VoxFooterColumn,
  ee as VoxGrid,
  X as VoxHeader,
  Me as VoxHero,
  W as VoxIcon,
  A as VoxInput,
  $t as VoxInputGroup,
  Ot as VoxLinkHub,
  Pe as VoxLinkHubItem,
  Ae as VoxLoader,
  Y as VoxMenu,
  st as VoxPagination,
  ke as VoxQuote,
  G as VoxRadio,
  Ge as VoxRadioGroup,
  Mt as VoxRecordList,
  te as VoxRecordListItem,
  ve as VoxSelect,
  H as VoxSeriesNav,
  J as VoxSidenav,
  fe as VoxSidenavGroup,
  ge as VoxSidenavItem,
  N as VoxSponsor,
  at as VoxSponsorTier,
  Le as VoxStat,
  oe as VoxStep,
  Pt as VoxStepIndicator,
  Xe as VoxSubnav,
  ue as VoxSwitch,
  be as VoxTab,
  me as VoxTabPanel,
  _t as VoxTabs,
  D as VoxTextarea,
  K as VoxThemeToggle,
  At as VoxTimeline,
  Ee as VoxTimelineItem,
  Je as VoxToc,
  ye as VoxTocItem
};
