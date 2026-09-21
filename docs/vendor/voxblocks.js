/**
 * @license
 * Copyright 2019 Google LLC
 * SPDX-License-Identifier: BSD-3-Clause
 */
const ct = globalThis, Kt = ct.ShadowRoot && (ct.ShadyCSS === void 0 || ct.ShadyCSS.nativeShadow) && "adoptedStyleSheets" in Document.prototype && "replace" in CSSStyleSheet.prototype, Wt = Symbol(), po = /* @__PURE__ */ new WeakMap();
let Po = class {
  constructor(e, r, s) {
    if (this._$cssResult$ = !0, s !== Wt) throw Error("CSSResult is not constructable. Use `unsafeCSS` or `css` instead.");
    this.cssText = e, this.t = r;
  }
  get styleSheet() {
    let e = this.o;
    const r = this.t;
    if (Kt && e === void 0) {
      const s = r !== void 0 && r.length === 1;
      s && (e = po.get(r)), e === void 0 && ((this.o = e = new CSSStyleSheet()).replaceSync(this.cssText), s && po.set(r, e));
    }
    return e;
  }
  toString() {
    return this.cssText;
  }
};
const Go = (o) => new Po(typeof o == "string" ? o : o + "", void 0, Wt), d = (o, ...e) => {
  const r = o.length === 1 ? o[0] : e.reduce((s, t, a) => s + ((i) => {
    if (i._$cssResult$ === !0) return i.cssText;
    if (typeof i == "number") return i;
    throw Error("Value passed to 'css' function must be a 'css' function result: " + i + ". Use 'unsafeCSS' to pass non-literal values, but take care to ensure page security.");
  })(t) + o[a + 1], o[0]);
  return new Po(r, o, Wt);
}, Yo = (o, e) => {
  if (Kt) o.adoptedStyleSheets = e.map((r) => r instanceof CSSStyleSheet ? r : r.styleSheet);
  else for (const r of e) {
    const s = document.createElement("style"), t = ct.litNonce;
    t !== void 0 && s.setAttribute("nonce", t), s.textContent = r.cssText, o.appendChild(s);
  }
}, vo = Kt ? (o) => o : (o) => o instanceof CSSStyleSheet ? ((e) => {
  let r = "";
  for (const s of e.cssRules) r += s.cssText;
  return Go(r);
})(o) : o;
/**
 * @license
 * Copyright 2017 Google LLC
 * SPDX-License-Identifier: BSD-3-Clause
 */
const { is: Jo, defineProperty: Xo, getOwnPropertyDescriptor: Qo, getOwnPropertyNames: er, getOwnPropertySymbols: tr, getPrototypeOf: or } = Object, N = globalThis, uo = N.trustedTypes, rr = uo ? uo.emptyScript : "", Dt = N.reactiveElementPolyfillSupport, Se = (o, e) => o, dt = { toAttribute(o, e) {
  switch (e) {
    case Boolean:
      o = o ? rr : null;
      break;
    case Object:
    case Array:
      o = o == null ? o : JSON.stringify(o);
  }
  return o;
}, fromAttribute(o, e) {
  let r = o;
  switch (e) {
    case Boolean:
      r = o !== null;
      break;
    case Number:
      r = o === null ? null : Number(o);
      break;
    case Object:
    case Array:
      try {
        r = JSON.parse(o);
      } catch {
        r = null;
      }
  }
  return r;
} }, Gt = (o, e) => !Jo(o, e), xo = { attribute: !0, type: String, converter: dt, reflect: !1, useDefault: !1, hasChanged: Gt };
Symbol.metadata ?? (Symbol.metadata = Symbol("metadata")), N.litPropertyMetadata ?? (N.litPropertyMetadata = /* @__PURE__ */ new WeakMap());
let fe = class extends HTMLElement {
  static addInitializer(e) {
    this._$Ei(), (this.l ?? (this.l = [])).push(e);
  }
  static get observedAttributes() {
    return this.finalize(), this._$Eh && [...this._$Eh.keys()];
  }
  static createProperty(e, r = xo) {
    if (r.state && (r.attribute = !1), this._$Ei(), this.prototype.hasOwnProperty(e) && ((r = Object.create(r)).wrapped = !0), this.elementProperties.set(e, r), !r.noAccessor) {
      const s = Symbol(), t = this.getPropertyDescriptor(e, s, r);
      t !== void 0 && Xo(this.prototype, e, t);
    }
  }
  static getPropertyDescriptor(e, r, s) {
    const { get: t, set: a } = Qo(this.prototype, e) ?? { get() {
      return this[r];
    }, set(i) {
      this[r] = i;
    } };
    return { get: t, set(i) {
      const u = t == null ? void 0 : t.call(this);
      a == null || a.call(this, i), this.requestUpdate(e, u, s);
    }, configurable: !0, enumerable: !0 };
  }
  static getPropertyOptions(e) {
    return this.elementProperties.get(e) ?? xo;
  }
  static _$Ei() {
    if (this.hasOwnProperty(Se("elementProperties"))) return;
    const e = or(this);
    e.finalize(), e.l !== void 0 && (this.l = [...e.l]), this.elementProperties = new Map(e.elementProperties);
  }
  static finalize() {
    if (this.hasOwnProperty(Se("finalized"))) return;
    if (this.finalized = !0, this._$Ei(), this.hasOwnProperty(Se("properties"))) {
      const r = this.properties, s = [...er(r), ...tr(r)];
      for (const t of s) this.createProperty(t, r[t]);
    }
    const e = this[Symbol.metadata];
    if (e !== null) {
      const r = litPropertyMetadata.get(e);
      if (r !== void 0) for (const [s, t] of r) this.elementProperties.set(s, t);
    }
    this._$Eh = /* @__PURE__ */ new Map();
    for (const [r, s] of this.elementProperties) {
      const t = this._$Eu(r, s);
      t !== void 0 && this._$Eh.set(t, r);
    }
    this.elementStyles = this.finalizeStyles(this.styles);
  }
  static finalizeStyles(e) {
    const r = [];
    if (Array.isArray(e)) {
      const s = new Set(e.flat(1 / 0).reverse());
      for (const t of s) r.unshift(vo(t));
    } else e !== void 0 && r.push(vo(e));
    return r;
  }
  static _$Eu(e, r) {
    const s = r.attribute;
    return s === !1 ? void 0 : typeof s == "string" ? s : typeof e == "string" ? e.toLowerCase() : void 0;
  }
  constructor() {
    super(), this._$Ep = void 0, this.isUpdatePending = !1, this.hasUpdated = !1, this._$Em = null, this._$Ev();
  }
  _$Ev() {
    var e;
    this._$ES = new Promise((r) => this.enableUpdating = r), this._$AL = /* @__PURE__ */ new Map(), this._$E_(), this.requestUpdate(), (e = this.constructor.l) == null || e.forEach((r) => r(this));
  }
  addController(e) {
    var r;
    (this._$EO ?? (this._$EO = /* @__PURE__ */ new Set())).add(e), this.renderRoot !== void 0 && this.isConnected && ((r = e.hostConnected) == null || r.call(e));
  }
  removeController(e) {
    var r;
    (r = this._$EO) == null || r.delete(e);
  }
  _$E_() {
    const e = /* @__PURE__ */ new Map(), r = this.constructor.elementProperties;
    for (const s of r.keys()) this.hasOwnProperty(s) && (e.set(s, this[s]), delete this[s]);
    e.size > 0 && (this._$Ep = e);
  }
  createRenderRoot() {
    const e = this.shadowRoot ?? this.attachShadow(this.constructor.shadowRootOptions);
    return Yo(e, this.constructor.elementStyles), e;
  }
  connectedCallback() {
    var e;
    this.renderRoot ?? (this.renderRoot = this.createRenderRoot()), this.enableUpdating(!0), (e = this._$EO) == null || e.forEach((r) => {
      var s;
      return (s = r.hostConnected) == null ? void 0 : s.call(r);
    });
  }
  enableUpdating(e) {
  }
  disconnectedCallback() {
    var e;
    (e = this._$EO) == null || e.forEach((r) => {
      var s;
      return (s = r.hostDisconnected) == null ? void 0 : s.call(r);
    });
  }
  attributeChangedCallback(e, r, s) {
    this._$AK(e, s);
  }
  _$ET(e, r) {
    var a;
    const s = this.constructor.elementProperties.get(e), t = this.constructor._$Eu(e, s);
    if (t !== void 0 && s.reflect === !0) {
      const i = (((a = s.converter) == null ? void 0 : a.toAttribute) !== void 0 ? s.converter : dt).toAttribute(r, s.type);
      this._$Em = e, i == null ? this.removeAttribute(t) : this.setAttribute(t, i), this._$Em = null;
    }
  }
  _$AK(e, r) {
    var a, i;
    const s = this.constructor, t = s._$Eh.get(e);
    if (t !== void 0 && this._$Em !== t) {
      const u = s.getPropertyOptions(t), x = typeof u.converter == "function" ? { fromAttribute: u.converter } : ((a = u.converter) == null ? void 0 : a.fromAttribute) !== void 0 ? u.converter : dt;
      this._$Em = t;
      const f = x.fromAttribute(r, u.type);
      this[t] = f ?? ((i = this._$Ej) == null ? void 0 : i.get(t)) ?? f, this._$Em = null;
    }
  }
  requestUpdate(e, r, s, t = !1, a) {
    var i;
    if (e !== void 0) {
      const u = this.constructor;
      if (t === !1 && (a = this[e]), s ?? (s = u.getPropertyOptions(e)), !((s.hasChanged ?? Gt)(a, r) || s.useDefault && s.reflect && a === ((i = this._$Ej) == null ? void 0 : i.get(e)) && !this.hasAttribute(u._$Eu(e, s)))) return;
      this.C(e, r, s);
    }
    this.isUpdatePending === !1 && (this._$ES = this._$EP());
  }
  C(e, r, { useDefault: s, reflect: t, wrapped: a }, i) {
    s && !(this._$Ej ?? (this._$Ej = /* @__PURE__ */ new Map())).has(e) && (this._$Ej.set(e, i ?? r ?? this[e]), a !== !0 || i !== void 0) || (this._$AL.has(e) || (this.hasUpdated || s || (r = void 0), this._$AL.set(e, r)), t === !0 && this._$Em !== e && (this._$Eq ?? (this._$Eq = /* @__PURE__ */ new Set())).add(e));
  }
  async _$EP() {
    this.isUpdatePending = !0;
    try {
      await this._$ES;
    } catch (r) {
      Promise.reject(r);
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
        const { wrapped: u } = i, x = this[a];
        u !== !0 || this._$AL.has(a) || x === void 0 || this.C(a, void 0, i, x);
      }
    }
    let e = !1;
    const r = this._$AL;
    try {
      e = this.shouldUpdate(r), e ? (this.willUpdate(r), (s = this._$EO) == null || s.forEach((t) => {
        var a;
        return (a = t.hostUpdate) == null ? void 0 : a.call(t);
      }), this.update(r)) : this._$EM();
    } catch (t) {
      throw e = !1, this._$EM(), t;
    }
    e && this._$AE(r);
  }
  willUpdate(e) {
  }
  _$AE(e) {
    var r;
    (r = this._$EO) == null || r.forEach((s) => {
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
    this._$Eq && (this._$Eq = this._$Eq.forEach((r) => this._$ET(r, this[r]))), this._$EM();
  }
  updated(e) {
  }
  firstUpdated(e) {
  }
};
fe.elementStyles = [], fe.shadowRootOptions = { mode: "open" }, fe[Se("elementProperties")] = /* @__PURE__ */ new Map(), fe[Se("finalized")] = /* @__PURE__ */ new Map(), Dt == null || Dt({ ReactiveElement: fe }), (N.reactiveElementVersions ?? (N.reactiveElementVersions = [])).push("2.1.2");
/**
 * @license
 * Copyright 2017 Google LLC
 * SPDX-License-Identifier: BSD-3-Clause
 */
const ze = globalThis, fo = (o) => o, ht = ze.trustedTypes, bo = ht ? ht.createPolicy("lit-html", { createHTML: (o) => o }) : void 0, Eo = "$lit$", I = `lit$${Math.random().toFixed(9).slice(2)}$`, So = "?" + I, sr = `<${So}>`, ee = document, je = () => ee.createComment(""), Ve = (o) => o === null || typeof o != "object" && typeof o != "function", Yt = Array.isArray, ar = (o) => Yt(o) || typeof (o == null ? void 0 : o[Symbol.iterator]) == "function", Tt = `[ 	
\f\r]`, Ee = /<(?:(!--|\/[^a-zA-Z])|(\/?[a-zA-Z][^>\s]*)|(\/?$))/g, go = /-->/g, mo = />/g, Y = RegExp(`>|${Tt}(?:([^\\s"'>=/]+)(${Tt}*=${Tt}*(?:[^ 	
\f\r"'\`<>=]|("|')|))|$)`, "g"), yo = /'/g, $o = /"/g, zo = /^(?:script|style|textarea|title)$/i, jo = (o) => (e, ...r) => ({ _$litType$: o, strings: e, values: r }), l = jo(1), c = jo(2), M = Symbol.for("lit-noChange"), p = Symbol.for("lit-nothing"), wo = /* @__PURE__ */ new WeakMap(), X = ee.createTreeWalker(ee, 129);
function Vo(o, e) {
  if (!Yt(o) || !o.hasOwnProperty("raw")) throw Error("invalid template strings array");
  return bo !== void 0 ? bo.createHTML(e) : e;
}
const ir = (o, e) => {
  const r = o.length - 1, s = [];
  let t, a = e === 2 ? "<svg>" : e === 3 ? "<math>" : "", i = Ee;
  for (let u = 0; u < r; u++) {
    const x = o[u];
    let f, $, b = -1, z = 0;
    for (; z < x.length && (i.lastIndex = z, $ = i.exec(x), $ !== null); ) z = i.lastIndex, i === Ee ? $[1] === "!--" ? i = go : $[1] !== void 0 ? i = mo : $[2] !== void 0 ? (zo.test($[2]) && (t = RegExp("</" + $[2], "g")), i = Y) : $[3] !== void 0 && (i = Y) : i === Y ? $[0] === ">" ? (i = t ?? Ee, b = -1) : $[1] === void 0 ? b = -2 : (b = i.lastIndex - $[2].length, f = $[1], i = $[3] === void 0 ? Y : $[3] === '"' ? $o : yo) : i === $o || i === yo ? i = Y : i === go || i === mo ? i = Ee : (i = Y, t = void 0);
    const H = i === Y && o[u + 1].startsWith("/>") ? " " : "";
    a += i === Ee ? x + sr : b >= 0 ? (s.push(f), x.slice(0, b) + Eo + x.slice(b) + I + H) : x + I + (b === -2 ? u : H);
  }
  return [Vo(o, a + (o[r] || "<?>") + (e === 2 ? "</svg>" : e === 3 ? "</math>" : "")), s];
};
class De {
  constructor({ strings: e, _$litType$: r }, s) {
    let t;
    this.parts = [];
    let a = 0, i = 0;
    const u = e.length - 1, x = this.parts, [f, $] = ir(e, r);
    if (this.el = De.createElement(f, s), X.currentNode = this.el.content, r === 2 || r === 3) {
      const b = this.el.content.firstChild;
      b.replaceWith(...b.childNodes);
    }
    for (; (t = X.nextNode()) !== null && x.length < u; ) {
      if (t.nodeType === 1) {
        if (t.hasAttributes()) for (const b of t.getAttributeNames()) if (b.endsWith(Eo)) {
          const z = $[i++], H = t.getAttribute(b).split(I), at = /([.?@])?(.*)/.exec(z);
          x.push({ type: 1, index: a, name: at[2], strings: H, ctor: at[1] === "." ? lr : at[1] === "?" ? cr : at[1] === "@" ? dr : Ct }), t.removeAttribute(b);
        } else b.startsWith(I) && (x.push({ type: 6, index: a }), t.removeAttribute(b));
        if (zo.test(t.tagName)) {
          const b = t.textContent.split(I), z = b.length - 1;
          if (z > 0) {
            t.textContent = ht ? ht.emptyScript : "";
            for (let H = 0; H < z; H++) t.append(b[H], je()), X.nextNode(), x.push({ type: 2, index: ++a });
            t.append(b[z], je());
          }
        }
      } else if (t.nodeType === 8) if (t.data === So) x.push({ type: 2, index: a });
      else {
        let b = -1;
        for (; (b = t.data.indexOf(I, b + 1)) !== -1; ) x.push({ type: 7, index: a }), b += I.length - 1;
      }
      a++;
    }
  }
  static createElement(e, r) {
    const s = ee.createElement("template");
    return s.innerHTML = e, s;
  }
}
function be(o, e, r = o, s) {
  var i, u;
  if (e === M) return e;
  let t = s !== void 0 ? (i = r._$Co) == null ? void 0 : i[s] : r._$Cl;
  const a = Ve(e) ? void 0 : e._$litDirective$;
  return (t == null ? void 0 : t.constructor) !== a && ((u = t == null ? void 0 : t._$AO) == null || u.call(t, !1), a === void 0 ? t = void 0 : (t = new a(o), t._$AT(o, r, s)), s !== void 0 ? (r._$Co ?? (r._$Co = []))[s] = t : r._$Cl = t), t !== void 0 && (e = be(o, t._$AS(o, e.values), t, s)), e;
}
class nr {
  constructor(e, r) {
    this._$AV = [], this._$AN = void 0, this._$AD = e, this._$AM = r;
  }
  get parentNode() {
    return this._$AM.parentNode;
  }
  get _$AU() {
    return this._$AM._$AU;
  }
  u(e) {
    const { el: { content: r }, parts: s } = this._$AD, t = ((e == null ? void 0 : e.creationScope) ?? ee).importNode(r, !0);
    X.currentNode = t;
    let a = X.nextNode(), i = 0, u = 0, x = s[0];
    for (; x !== void 0; ) {
      if (i === x.index) {
        let f;
        x.type === 2 ? f = new et(a, a.nextSibling, this, e) : x.type === 1 ? f = new x.ctor(a, x.name, x.strings, this, e) : x.type === 6 && (f = new hr(a, this, e)), this._$AV.push(f), x = s[++u];
      }
      i !== (x == null ? void 0 : x.index) && (a = X.nextNode(), i++);
    }
    return X.currentNode = ee, t;
  }
  p(e) {
    let r = 0;
    for (const s of this._$AV) s !== void 0 && (s.strings !== void 0 ? (s._$AI(e, s, r), r += s.strings.length - 2) : s._$AI(e[r])), r++;
  }
}
class et {
  get _$AU() {
    var e;
    return ((e = this._$AM) == null ? void 0 : e._$AU) ?? this._$Cv;
  }
  constructor(e, r, s, t) {
    this.type = 2, this._$AH = p, this._$AN = void 0, this._$AA = e, this._$AB = r, this._$AM = s, this.options = t, this._$Cv = (t == null ? void 0 : t.isConnected) ?? !0;
  }
  get parentNode() {
    let e = this._$AA.parentNode;
    const r = this._$AM;
    return r !== void 0 && (e == null ? void 0 : e.nodeType) === 11 && (e = r.parentNode), e;
  }
  get startNode() {
    return this._$AA;
  }
  get endNode() {
    return this._$AB;
  }
  _$AI(e, r = this) {
    e = be(this, e, r), Ve(e) ? e === p || e == null || e === "" ? (this._$AH !== p && this._$AR(), this._$AH = p) : e !== this._$AH && e !== M && this._(e) : e._$litType$ !== void 0 ? this.$(e) : e.nodeType !== void 0 ? this.T(e) : ar(e) ? this.k(e) : this._(e);
  }
  O(e) {
    return this._$AA.parentNode.insertBefore(e, this._$AB);
  }
  T(e) {
    this._$AH !== e && (this._$AR(), this._$AH = this.O(e));
  }
  _(e) {
    this._$AH !== p && Ve(this._$AH) ? this._$AA.nextSibling.data = e : this.T(ee.createTextNode(e)), this._$AH = e;
  }
  $(e) {
    var a;
    const { values: r, _$litType$: s } = e, t = typeof s == "number" ? this._$AC(e) : (s.el === void 0 && (s.el = De.createElement(Vo(s.h, s.h[0]), this.options)), s);
    if (((a = this._$AH) == null ? void 0 : a._$AD) === t) this._$AH.p(r);
    else {
      const i = new nr(t, this), u = i.u(this.options);
      i.p(r), this.T(u), this._$AH = i;
    }
  }
  _$AC(e) {
    let r = wo.get(e.strings);
    return r === void 0 && wo.set(e.strings, r = new De(e)), r;
  }
  k(e) {
    Yt(this._$AH) || (this._$AH = [], this._$AR());
    const r = this._$AH;
    let s, t = 0;
    for (const a of e) t === r.length ? r.push(s = new et(this.O(je()), this.O(je()), this, this.options)) : s = r[t], s._$AI(a), t++;
    t < r.length && (this._$AR(s && s._$AB.nextSibling, t), r.length = t);
  }
  _$AR(e = this._$AA.nextSibling, r) {
    var s;
    for ((s = this._$AP) == null ? void 0 : s.call(this, !1, !0, r); e !== this._$AB; ) {
      const t = fo(e).nextSibling;
      fo(e).remove(), e = t;
    }
  }
  setConnected(e) {
    var r;
    this._$AM === void 0 && (this._$Cv = e, (r = this._$AP) == null || r.call(this, e));
  }
}
class Ct {
  get tagName() {
    return this.element.tagName;
  }
  get _$AU() {
    return this._$AM._$AU;
  }
  constructor(e, r, s, t, a) {
    this.type = 1, this._$AH = p, this._$AN = void 0, this.element = e, this.name = r, this._$AM = t, this.options = a, s.length > 2 || s[0] !== "" || s[1] !== "" ? (this._$AH = Array(s.length - 1).fill(new String()), this.strings = s) : this._$AH = p;
  }
  _$AI(e, r = this, s, t) {
    const a = this.strings;
    let i = !1;
    if (a === void 0) e = be(this, e, r, 0), i = !Ve(e) || e !== this._$AH && e !== M, i && (this._$AH = e);
    else {
      const u = e;
      let x, f;
      for (e = a[0], x = 0; x < a.length - 1; x++) f = be(this, u[s + x], r, x), f === M && (f = this._$AH[x]), i || (i = !Ve(f) || f !== this._$AH[x]), f === p ? e = p : e !== p && (e += (f ?? "") + a[x + 1]), this._$AH[x] = f;
    }
    i && !t && this.j(e);
  }
  j(e) {
    e === p ? this.element.removeAttribute(this.name) : this.element.setAttribute(this.name, e ?? "");
  }
}
class lr extends Ct {
  constructor() {
    super(...arguments), this.type = 3;
  }
  j(e) {
    this.element[this.name] = e === p ? void 0 : e;
  }
}
class cr extends Ct {
  constructor() {
    super(...arguments), this.type = 4;
  }
  j(e) {
    this.element.toggleAttribute(this.name, !!e && e !== p);
  }
}
class dr extends Ct {
  constructor(e, r, s, t, a) {
    super(e, r, s, t, a), this.type = 5;
  }
  _$AI(e, r = this) {
    if ((e = be(this, e, r, 0) ?? p) === M) return;
    const s = this._$AH, t = e === p && s !== p || e.capture !== s.capture || e.once !== s.once || e.passive !== s.passive, a = e !== p && (s === p || t);
    t && this.element.removeEventListener(this.name, this, s), a && this.element.addEventListener(this.name, this, e), this._$AH = e;
  }
  handleEvent(e) {
    var r;
    typeof this._$AH == "function" ? this._$AH.call(((r = this.options) == null ? void 0 : r.host) ?? this.element, e) : this._$AH.handleEvent(e);
  }
}
class hr {
  constructor(e, r, s) {
    this.element = e, this.type = 6, this._$AN = void 0, this._$AM = r, this.options = s;
  }
  get _$AU() {
    return this._$AM._$AU;
  }
  _$AI(e) {
    be(this, e);
  }
}
const Ht = ze.litHtmlPolyfillSupport;
Ht == null || Ht(De, et), (ze.litHtmlVersions ?? (ze.litHtmlVersions = [])).push("3.3.3");
const pr = (o, e, r) => {
  const s = (r == null ? void 0 : r.renderBefore) ?? e;
  let t = s._$litPart$;
  if (t === void 0) {
    const a = (r == null ? void 0 : r.renderBefore) ?? null;
    s._$litPart$ = t = new et(e.insertBefore(je(), a), a, void 0, r ?? {});
  }
  return t._$AI(o), t;
};
/**
 * @license
 * Copyright 2017 Google LLC
 * SPDX-License-Identifier: BSD-3-Clause
 */
const Q = globalThis;
let v = class extends fe {
  constructor() {
    super(...arguments), this.renderOptions = { host: this }, this._$Do = void 0;
  }
  createRenderRoot() {
    var r;
    const e = super.createRenderRoot();
    return (r = this.renderOptions).renderBefore ?? (r.renderBefore = e.firstChild), e;
  }
  update(e) {
    const r = this.render();
    this.hasUpdated || (this.renderOptions.isConnected = this.isConnected), super.update(e), this._$Do = pr(r, this.renderRoot, this.renderOptions);
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
    return M;
  }
};
var Ao;
v._$litElement$ = !0, v.finalized = !0, (Ao = Q.litElementHydrateSupport) == null || Ao.call(Q, { LitElement: v });
const It = Q.litElementPolyfillSupport;
It == null || It({ LitElement: v });
(Q.litElementVersions ?? (Q.litElementVersions = [])).push("4.2.2");
/**
 * @license
 * Copyright 2017 Google LLC
 * SPDX-License-Identifier: BSD-3-Clause
 */
const h = (o) => (e, r) => {
  r !== void 0 ? r.addInitializer(() => {
    customElements.define(o, e);
  }) : customElements.define(o, e);
};
/**
 * @license
 * Copyright 2017 Google LLC
 * SPDX-License-Identifier: BSD-3-Clause
 */
const vr = { attribute: !0, type: String, converter: dt, reflect: !1, hasChanged: Gt }, ur = (o = vr, e, r) => {
  const { kind: s, metadata: t } = r;
  let a = globalThis.litPropertyMetadata.get(t);
  if (a === void 0 && globalThis.litPropertyMetadata.set(t, a = /* @__PURE__ */ new Map()), s === "setter" && ((o = Object.create(o)).wrapped = !0), a.set(r.name, o), s === "accessor") {
    const { name: i } = r;
    return { set(u) {
      const x = e.get.call(this);
      e.set.call(this, u), this.requestUpdate(i, x, o, !0, u);
    }, init(u) {
      return u !== void 0 && this.C(i, void 0, o, u), u;
    } };
  }
  if (s === "setter") {
    const { name: i } = r;
    return function(u) {
      const x = this[i];
      e.call(this, u), this.requestUpdate(i, x, o, !0, u);
    };
  }
  throw Error("Unsupported decorator location: " + s);
};
function n(o) {
  return (e, r) => typeof r == "object" ? ur(o, e, r) : ((s, t, a) => {
    const i = t.hasOwnProperty(a);
    return t.constructor.createProperty(a, s), i ? Object.getOwnPropertyDescriptor(t, a) : void 0;
  })(o, e, r);
}
/**
 * @license
 * Copyright 2017 Google LLC
 * SPDX-License-Identifier: BSD-3-Clause
 */
function A(o) {
  return n({ ...o, state: !0, attribute: !1 });
}
/**
 * @license
 * Copyright 2017 Google LLC
 * SPDX-License-Identifier: BSD-3-Clause
 */
const xr = (o, e, r) => (r.configurable = !0, r.enumerable = !0, Reflect.decorate && typeof e != "object" && Object.defineProperty(o, e, r), r);
/**
 * @license
 * Copyright 2017 Google LLC
 * SPDX-License-Identifier: BSD-3-Clause
 */
function ke(o, e) {
  return (r, s, t) => {
    const a = (i) => {
      var u;
      return ((u = i.renderRoot) == null ? void 0 : u.querySelector(o)) ?? null;
    };
    return xr(r, s, { get() {
      return a(this);
    } });
  };
}
/**
 * @license
 * Copyright 2017 Google LLC
 * SPDX-License-Identifier: BSD-3-Clause
 */
const J = { ATTRIBUTE: 1, PROPERTY: 3, BOOLEAN_ATTRIBUTE: 4 }, Do = (o) => (...e) => ({ _$litDirective$: o, values: e });
class To {
  constructor(e) {
  }
  get _$AU() {
    return this._$AM._$AU;
  }
  _$AT(e, r, s) {
    this._$Ct = e, this._$AM = r, this._$Ci = s;
  }
  _$AS(e, r) {
    return this.update(e, r);
  }
  update(e, r) {
    return this.render(...r);
  }
}
/**
 * @license
 * Copyright 2018 Google LLC
 * SPDX-License-Identifier: BSD-3-Clause
 */
const Ho = Do(class extends To {
  constructor(o) {
    var e;
    if (super(o), o.type !== J.ATTRIBUTE || o.name !== "class" || ((e = o.strings) == null ? void 0 : e.length) > 2) throw Error("`classMap()` can only be used in the `class` attribute and must be the only part in the attribute.");
  }
  render(o) {
    return " " + Object.keys(o).filter((e) => o[e]).join(" ") + " ";
  }
  update(o, [e]) {
    var s, t;
    if (this.st === void 0) {
      this.st = /* @__PURE__ */ new Set(), o.strings !== void 0 && (this.nt = new Set(o.strings.join(" ").split(/\s/).filter((a) => a !== "")));
      for (const a in e) e[a] && !((s = this.nt) != null && s.has(a)) && this.st.add(a);
      return this.render(e);
    }
    const r = o.element.classList;
    for (const a of this.st) a in e || (r.remove(a), this.st.delete(a));
    for (const a in e) {
      const i = !!e[a];
      i === this.st.has(a) || (t = this.nt) != null && t.has(a) || (i ? (r.add(a), this.st.add(a)) : (r.remove(a), this.st.delete(a)));
    }
    return M;
  }
});
/**
 * @license
 * Copyright 2018 Google LLC
 * SPDX-License-Identifier: BSD-3-Clause
 */
const g = (o) => o ?? p;
var fr = Object.defineProperty, br = Object.getOwnPropertyDescriptor, ne = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? br(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && fr(e, r, t), t;
};
let j = class extends v {
  constructor() {
    super(...arguments), this.variant = "brand", this.size = "md", this.type = "button", this.disabled = !1;
  }
  render() {
    const o = Ho({
      button: !0,
      [this.variant]: !0,
      [this.size]: !0
    });
    return this.href !== void 0 && !this.disabled ? l`
        <a
          class=${o}
          href=${this.href}
          target=${g(this.target)}
          rel=${g(this.target === "_blank" ? "noreferrer" : void 0)}
        >
          <slot></slot>
        </a>
      ` : l`
      <button class=${o} type=${this.type} ?disabled=${this.disabled}>
        <slot></slot>
      </button>
    `;
  }
};
j.styles = d`
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
ne([
  n()
], j.prototype, "variant", 2);
ne([
  n()
], j.prototype, "size", 2);
ne([
  n()
], j.prototype, "href", 2);
ne([
  n()
], j.prototype, "target", 2);
ne([
  n()
], j.prototype, "type", 2);
ne([
  n({ type: Boolean, reflect: !0 })
], j.prototype, "disabled", 2);
j = ne([
  h("vox-button")
], j);
var gr = Object.defineProperty, mr = Object.getOwnPropertyDescriptor, Jt = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? mr(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && gr(e, r, t), t;
};
let Te = class extends v {
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
Te.styles = d`
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
Jt([
  n()
], Te.prototype, "href", 2);
Jt([
  n()
], Te.prototype, "target", 2);
Te = Jt([
  h("vox-cta")
], Te);
var yr = Object.defineProperty, $r = Object.getOwnPropertyDescriptor, Ot = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? $r(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && yr(e, r, t), t;
};
const _o = "vox-theme", it = "data-vox-theme";
let ge = class extends v {
  constructor() {
    super(...arguments), this.lightLabel = "Switch to dark theme", this.darkLabel = "Switch to light theme", this.dark = !1, this.media = window.matchMedia("(prefers-color-scheme: dark)"), this.handleSystemChange = (o) => {
      localStorage.getItem(_o) || document.documentElement.setAttribute(it, o.matches ? "dark" : "light");
    };
  }
  connectedCallback() {
    super.connectedCallback(), this.syncFromDocument(), this.observer = new MutationObserver(() => this.syncFromDocument()), this.observer.observe(document.documentElement, {
      attributes: !0,
      attributeFilter: [it]
    }), this.media.addEventListener("change", this.handleSystemChange);
  }
  disconnectedCallback() {
    var o;
    super.disconnectedCallback(), (o = this.observer) == null || o.disconnect(), this.media.removeEventListener("change", this.handleSystemChange);
  }
  syncFromDocument() {
    const o = document.documentElement.getAttribute(it) === "dark";
    this.dark = o, this.toggleAttribute("dark", o);
  }
  handleClick() {
    const o = this.dark ? "light" : "dark";
    document.documentElement.setAttribute(it, o), localStorage.setItem(_o, o), this.dispatchEvent(
      new CustomEvent("vox-theme-change", {
        detail: { theme: o },
        bubbles: !0,
        composed: !0
      })
    );
  }
  render() {
    const o = this.dark ? this.darkLabel : this.lightLabel;
    return l`
      <button
        type="button"
        role="switch"
        aria-checked=${this.dark}
        aria-label=${o}
        title=${o}
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
ge.styles = d`
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
Ot([
  n({ attribute: "light-label" })
], ge.prototype, "lightLabel", 2);
Ot([
  n({ attribute: "dark-label" })
], ge.prototype, "darkLabel", 2);
Ot([
  A()
], ge.prototype, "dark", 2);
ge = Ot([
  h("vox-theme-toggle")
], ge);
const q = {
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
var wr = Object.defineProperty, _r = Object.getOwnPropertyDescriptor, kt = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? _r(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && wr(e, r, t), t;
};
let me = class extends v {
  constructor() {
    super(...arguments), this.name = "info", this.size = "md";
  }
  render() {
    const o = q[this.name];
    return o ? l`
      <svg
        viewBox="0 0 48 48"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
        stroke-linecap="round"
        stroke-linejoin="round"
        role=${this.label ? "img" : p}
        aria-hidden=${this.label ? p : "true"}
        aria-label=${this.label ?? p}
      >
        ${o}
      </svg>
    ` : p;
  }
};
me.styles = d`
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
kt([
  n()
], me.prototype, "name", 2);
kt([
  n({ reflect: !0 })
], me.prototype, "size", 2);
kt([
  n()
], me.prototype, "label", 2);
me = kt([
  h("vox-icon")
], me);
var Cr = Object.defineProperty, le = (o, e, r, s) => {
  for (var t = void 0, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = i(e, r, t) || t);
  return t && Cr(e, r, t), t;
};
const ho = class ho extends v {
  constructor() {
    super(), this.invalid = !1, this.name = "", this.label = "", this.note = "", this.disabled = !1, this.required = !1, this.hostLabel = "", this.internals = this.attachInternals();
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
    this.internals.setValidity(e.validity, e.validationMessage, e), this.invalid = !e.validity.valid;
  }
  renderLabel(e) {
    return this.label ? l`
      <label class="label" for=${e}>
        ${this.label}${this.required ? l`<span class="required-mark" aria-hidden="true"> *</span>` : p}
      </label>
    ` : p;
  }
  /**
   * Accessible name for the inner control when no visible `label` is set:
   * a host `aria-label` if there is one, else the component's own
   * translatable default.
   */
  fallbackName(e) {
    return this.hostLabel || e;
  }
  /** `note`'s id when present, for `aria-describedby` on the native control. */
  get noteId() {
    return this.note ? "note" : void 0;
  }
  renderNote() {
    return this.note ? l`<p class="note" id="note">${this.note}</p>` : p;
  }
};
ho.formAssociated = !0;
let m = ho;
le([
  A()
], m.prototype, "invalid");
le([
  n()
], m.prototype, "name");
le([
  n()
], m.prototype, "label");
le([
  n()
], m.prototype, "note");
le([
  n({ type: Boolean, reflect: !0 })
], m.prototype, "disabled");
le([
  n({ type: Boolean, reflect: !0 })
], m.prototype, "required");
le([
  n({ attribute: "aria-label" })
], m.prototype, "hostLabel");
const ce = d`
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

  .control[aria-invalid='true'] {
    border-color: var(--vox-color-danger-1);
  }

  .control[aria-invalid='true']:focus {
    border-color: var(--vox-color-danger-1);
    box-shadow: 0 0 0 3px var(--vox-color-danger-soft);
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
`, Io = d`
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
var Or = Object.defineProperty, kr = Object.getOwnPropertyDescriptor, Mt = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? kr(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && Or(e, r, t), t;
};
let ye = class extends m {
  constructor() {
    super(...arguments), this.checked = !1, this.value = "on", this.requiredMessage = "Please check this box.";
  }
  formResetCallback() {
    this.checked = !1;
  }
  updated() {
    this.internals.setFormValue(this.checked ? this.value : null), this.invalid = this.required && !this.checked, this.internals.setValidity(
      this.invalid ? { valueMissing: !0 } : {},
      this.requiredMessage,
      this.renderRoot.querySelector("input") ?? void 0
    );
  }
  handleChange(o) {
    this.checked = o.target.checked, this.dispatchEvent(new Event("change", { bubbles: !0 }));
  }
  render() {
    return l`
      <label class="check">
        <input
          type="checkbox"
          .checked=${this.checked}
          ?disabled=${this.disabled}
          ?required=${this.required}
          aria-invalid=${this.invalid ? "true" : "false"}
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
ye.styles = [
  Io,
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
Mt([
  n({ type: Boolean, reflect: !0 })
], ye.prototype, "checked", 2);
Mt([
  n()
], ye.prototype, "value", 2);
Mt([
  n({ attribute: "required-message" })
], ye.prototype, "requiredMessage", 2);
ye = Mt([
  h("vox-checkbox")
], ye);
/**
 * @license
 * Copyright 2020 Google LLC
 * SPDX-License-Identifier: BSD-3-Clause
 */
const Mr = (o) => o.strings === void 0, Lr = {}, Ar = (o, e = Lr) => o._$AH = e;
/**
 * @license
 * Copyright 2020 Google LLC
 * SPDX-License-Identifier: BSD-3-Clause
 */
const Lt = Do(class extends To {
  constructor(o) {
    if (super(o), o.type !== J.PROPERTY && o.type !== J.ATTRIBUTE && o.type !== J.BOOLEAN_ATTRIBUTE) throw Error("The `live` directive is not allowed on child or event bindings");
    if (!Mr(o)) throw Error("`live` bindings can only contain a single expression");
  }
  render(o) {
    return o;
  }
  update(o, [e]) {
    if (e === M || e === p) return e;
    const r = o.element, s = o.name;
    if (o.type === J.PROPERTY) {
      if (e === r[s]) return M;
    } else if (o.type === J.BOOLEAN_ATTRIBUTE) {
      if (!!e === r.hasAttribute(s)) return M;
    } else if (o.type === J.ATTRIBUTE && r.getAttribute(s) === e + "") return M;
    return Ar(o), e;
  }
});
var Pr = Object.defineProperty, Er = Object.getOwnPropertyDescriptor, _ = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? Er(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && Pr(e, r, t), t;
};
let y = class extends m {
  constructor() {
    super(...arguments), this.value = "", this.allowCustom = !1, this.emptyText = "No matches", this.fallbackLabel = "combobox", this.optionsLabel = "options", this.requiredMessage = "Please select an option.", this.countText = "{n} options available", this.countTextOne = "{n} option available", this.query = "", this.open = !1, this.activeIndex = -1, this.options = [];
  }
  get filtered() {
    const o = this.query.trim().toLowerCase();
    return !o || o === this.selectedLabel.toLowerCase() ? this.options : this.options.filter((e) => e.label.toLowerCase().includes(o));
  }
  get selectedLabel() {
    var o;
    return ((o = this.options.find((e) => e.value === this.value)) == null ? void 0 : o.label) ?? "";
  }
  willUpdate(o) {
    var e;
    o.has("value") && ((e = this.shadowRoot) == null ? void 0 : e.activeElement) === null && (this.query = this.selectedLabel);
  }
  formResetCallback() {
    this.value = "", this.query = "", this.close();
  }
  updated() {
    this.internals.setFormValue(this.value || null), this.invalid = this.required && !this.value, this.internals.setValidity(
      this.invalid ? { valueMissing: !0 } : {},
      this.requiredMessage,
      this.inputEl
    );
  }
  focus(o) {
    var e;
    (e = this.inputEl) == null || e.focus(o);
  }
  readOptions() {
    this.options = [...this.querySelectorAll("option")].map((o) => {
      var e;
      return {
        value: o.value,
        label: ((e = o.textContent) == null ? void 0 : e.trim()) ?? "",
        disabled: o.disabled
      };
    }), this.value && !this.query && (this.query = this.selectedLabel);
  }
  openList() {
    if (this.open || this.disabled) return;
    this.open = !0;
    const o = this.filtered.findIndex((e) => e.value === this.value);
    this.activeIndex = o;
  }
  close() {
    this.open = !1, this.activeIndex = -1;
  }
  select(o) {
    o.disabled || (this.value = o.value, this.query = o.label, this.close(), this.dispatchEvent(new Event("change", { bubbles: !0 })));
  }
  moveActive(o) {
    const e = this.filtered;
    if (e.length === 0 || e.filter((t) => !t.disabled).length === 0) return;
    let s = this.activeIndex;
    for (let t = 0; t < e.length && (s = (s + o + e.length) % e.length, !!e[s].disabled); t++)
      ;
    this.activeIndex = s, this.scrollActiveIntoView();
  }
  scrollActiveIntoView() {
    this.updateComplete.then(() => {
      var o, e;
      (e = (o = this.listboxEl) == null ? void 0 : o.querySelector("[data-active]")) == null || e.scrollIntoView({ block: "nearest" });
    });
  }
  handleInput(o) {
    this.query = o.target.value, this.openList(), this.activeIndex = -1, this.allowCustom && (this.value = this.query);
  }
  handleKeydown(o) {
    switch (o.key) {
      case "ArrowDown":
        o.preventDefault(), this.open ? this.moveActive(1) : (this.openList(), this.activeIndex === -1 && this.moveActive(1));
        break;
      case "ArrowUp":
        o.preventDefault(), this.open || (this.openList(), this.activeIndex = this.filtered.length), this.moveActive(-1);
        break;
      case "Home":
        if (!this.open) return;
        o.preventDefault(), this.activeIndex = -1, this.moveActive(1);
        break;
      case "End":
        if (!this.open) return;
        o.preventDefault(), this.activeIndex = this.filtered.length, this.moveActive(-1);
        break;
      case "Enter": {
        if (!this.open) return;
        const e = this.filtered[this.activeIndex];
        e && (o.preventDefault(), this.select(e));
        break;
      }
      case "Escape":
        o.preventDefault(), this.open ? this.close() : (this.value || this.query) && (this.query = "", this.value = "", this.dispatchEvent(new Event("change", { bubbles: !0 })));
        break;
      case "Tab":
        this.close();
        break;
    }
  }
  handleBlur() {
    if (this.close(), !this.allowCustom) {
      if (this.query === "") {
        this.value && (this.value = "", this.dispatchEvent(new Event("change", { bubbles: !0 })));
        return;
      }
      this.query = this.selectedLabel;
    }
  }
  formatCount(o) {
    return (o === 1 ? this.countTextOne : this.countText).replace("{n}", String(o));
  }
  optionId(o) {
    return `option-${o}`;
  }
  render() {
    const o = this.filtered, e = this.open && this.activeIndex >= 0 ? this.optionId(this.activeIndex) : void 0;
    return l`
      <div class="field">
        ${this.renderLabel("combobox")}
        <div class="combo">
          <input
            id="combobox"
            class="control"
            type="text"
            role="combobox"
            .value=${Lt(this.query)}
            placeholder=${g(this.placeholder)}
            autocomplete="off"
            aria-expanded=${this.open ? "true" : "false"}
            aria-controls="listbox"
            aria-autocomplete="list"
            aria-activedescendant=${g(e)}
            aria-label=${this.label ? p : this.fallbackName(this.fallbackLabel)}
            aria-describedby=${g(this.noteId)}
            aria-invalid=${this.invalid ? "true" : "false"}
            ?required=${this.required}
            ?disabled=${this.disabled}
            @input=${this.handleInput}
            @keydown=${this.handleKeydown}
            @blur=${this.handleBlur}
            @mousedown=${this.openList}
          />
          <ul
            class="listbox"
            id="listbox"
            role="listbox"
            aria-label=${this.label || this.hostLabel || this.optionsLabel}
            ?hidden=${!this.open}
          >
            ${o.length === 0 ? l`<li class="empty" role="presentation">${this.emptyText}</li>` : o.map(
      (r, s) => l`
                    <li
                      id=${this.optionId(s)}
                      class=${Ho({ option: !0 })}
                      role="option"
                      aria-selected=${r.value === this.value ? "true" : "false"}
                      aria-disabled=${r.disabled ? "true" : "false"}
                      ?data-active=${s === this.activeIndex}
                      @mousedown=${(t) => {
        t.preventDefault(), this.select(r);
      }}
                    >
                      ${r.label}
                    </li>
                  `
    )}
          </ul>
        </div>
        ${this.renderNote()}
        <span class="sr-only" role="status" aria-live="polite">
          ${this.open ? this.formatCount(o.length) : ""}
        </span>
      </div>
      <div hidden><slot @slotchange=${this.readOptions}></slot></div>
    `;
  }
};
y.styles = [
  ce,
  d`
      /*
       * The popup anchors to this shell, not to .field: an absolutely
       * positioned child of a flex container takes its static position from
       * the container's content-box origin, which would put it over the label.
       */
      .combo {
        position: relative;
      }

      .control {
        padding-right: var(--vox-space-8);
        background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='16' height='16' viewBox='0 0 24 24' fill='none' stroke='%23808080' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'%3E%3Cpath d='m6 9 6 6 6-6'/%3E%3C/svg%3E");
        background-repeat: no-repeat;
        background-position: right var(--vox-space-3) center;
      }

      .listbox {
        position: absolute;
        z-index: 20;
        top: calc(100% + var(--vox-space-1));
        left: 0;
        right: 0;
        margin: 0;
        padding: var(--vox-space-1);
        max-height: 15rem;
        overflow-y: auto;
        list-style: none;
        background-color: var(--vox-color-bg-elv);
        border: 1px solid var(--vox-color-border);
        border-radius: var(--vox-radius-md);
        box-shadow: 0 4px 12px rgb(0 0 0 / 12%);
      }

      .option {
        padding: var(--vox-space-2) var(--vox-space-3);
        border-radius: var(--vox-radius-sm);
        font-size: 14px;
        line-height: 1.5;
        color: var(--vox-color-text-1);
        cursor: pointer;
      }

      /*
       * Focus stays on the input, so the active option is styled rather than
       * focused. Colour alone can't carry it (WCAG 1.4.1) — the selected
       * option also gets a check mark.
       */
      .option[data-active] {
        background-color: var(--vox-color-brand-soft);
        outline: 2px solid var(--vox-color-brand-1);
        outline-offset: -2px;
      }

      .option[aria-selected='true'] {
        font-weight: 600;
      }

      .option[aria-selected='true']::after {
        content: ' ✓';
        color: var(--vox-color-brand-1);
      }

      .option[aria-disabled='true'] {
        color: var(--vox-color-text-3);
        cursor: not-allowed;
      }

      .empty {
        padding: var(--vox-space-2) var(--vox-space-3);
        font-size: 14px;
        color: var(--vox-color-text-2);
      }

      /* Announced, never shown. */
      .sr-only {
        position: absolute;
        width: 1px;
        height: 1px;
        padding: 0;
        margin: -1px;
        overflow: hidden;
        clip-path: inset(50%);
        white-space: nowrap;
      }
    `
];
_([
  n()
], y.prototype, "value", 2);
_([
  n()
], y.prototype, "placeholder", 2);
_([
  n({ type: Boolean, attribute: "allow-custom" })
], y.prototype, "allowCustom", 2);
_([
  n({ attribute: "empty-text" })
], y.prototype, "emptyText", 2);
_([
  n({ attribute: "fallback-label" })
], y.prototype, "fallbackLabel", 2);
_([
  n({ attribute: "options-label" })
], y.prototype, "optionsLabel", 2);
_([
  n({ attribute: "required-message" })
], y.prototype, "requiredMessage", 2);
_([
  n({ attribute: "count-text" })
], y.prototype, "countText", 2);
_([
  n({ attribute: "count-text-one" })
], y.prototype, "countTextOne", 2);
_([
  A()
], y.prototype, "query", 2);
_([
  A()
], y.prototype, "open", 2);
_([
  A()
], y.prototype, "activeIndex", 2);
_([
  A()
], y.prototype, "options", 2);
_([
  ke("input")
], y.prototype, "inputEl", 2);
_([
  ke(".listbox")
], y.prototype, "listboxEl", 2);
y = _([
  h("vox-combobox")
], y);
var Sr = Object.defineProperty, zr = Object.getOwnPropertyDescriptor, Z = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? zr(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && Sr(e, r, t), t;
};
let P = class extends m {
  constructor() {
    super(...arguments), this.multiple = !1, this.buttonLabel = "Choose a file", this.emptyText = "No file selected", this.requiredMessage = "Please select a file.", this.fileNames = [];
  }
  formResetCallback() {
    this.fileNames = [], this.inputEl && (this.inputEl.value = ""), this.internals.setFormValue(null);
  }
  /*
   * A required field is invalid from first render, not only once the user
   * has been through the picker — otherwise an untouched form submits.
   * Also called straight from handleChange so a `change` listener reading
   * checkValidity() sees the new state rather than the previous render's.
   */
  updated() {
    this.syncRequired();
  }
  syncRequired() {
    this.invalid = this.required && this.fileNames.length === 0, this.internals.setValidity(
      this.invalid ? { valueMissing: !0 } : {},
      this.requiredMessage,
      this.inputEl
    );
  }
  handleChange() {
    const o = [...this.inputEl.files ?? []];
    this.fileNames = o.map((r) => r.name);
    const e = new FormData();
    for (const r of o) e.append(this.name, r);
    this.internals.setFormValue(o.length > 0 ? e : null), this.syncRequired(), this.dispatchEvent(new Event("change", { bubbles: !0 }));
  }
  render() {
    return l`
      <div class="field">
        ${this.renderLabel("file")}
        <label>
          <input
            id="file"
            type="file"
            accept=${g(this.accept)}
            ?multiple=${this.multiple}
            ?disabled=${this.disabled}
            aria-describedby=${g(this.noteId)}
            aria-invalid=${this.invalid ? "true" : "false"}
            @change=${this.handleChange}
          />
          <span class="picker">
            <span class="button">${this.buttonLabel}</span>
            <span class="names">
              ${this.fileNames.length > 0 ? this.fileNames.join(", ") : this.emptyText}
            </span>
          </span>
        </label>
        ${this.renderNote()}
      </div>
    `;
  }
};
P.styles = [
  ce,
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
Z([
  n()
], P.prototype, "accept", 2);
Z([
  n({ type: Boolean })
], P.prototype, "multiple", 2);
Z([
  n({ attribute: "button-label" })
], P.prototype, "buttonLabel", 2);
Z([
  n({ attribute: "empty-text" })
], P.prototype, "emptyText", 2);
Z([
  n({ attribute: "required-message" })
], P.prototype, "requiredMessage", 2);
Z([
  A()
], P.prototype, "fileNames", 2);
Z([
  ke("input")
], P.prototype, "inputEl", 2);
P = Z([
  h("vox-file-input")
], P);
var jr = Object.defineProperty, Vr = Object.getOwnPropertyDescriptor, O = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? Vr(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && jr(e, r, t), t;
};
const Dr = {
  text: "text input",
  email: "email address",
  number: "number",
  password: "password",
  search: "search",
  tel: "telephone number",
  url: "web address",
  date: "date",
  time: "time",
  "datetime-local": "date and time",
  month: "month",
  week: "week"
};
let w = class extends m {
  constructor() {
    super(...arguments), this.type = "text", this.value = "", this.readonly = !1, this.fallbackLabel = "";
  }
  get accessibleName() {
    return this.fallbackName(
      this.fallbackLabel || Dr[this.type] || `${this.type} input`
    );
  }
  formResetCallback() {
    this.value = "";
  }
  updated() {
    this.internals.setFormValue(this.value);
    const o = this.renderRoot.querySelector("input");
    o && this.syncValidity(o);
  }
  focus(o) {
    var e;
    (e = this.renderRoot.querySelector("input")) == null || e.focus(o);
  }
  handleInput(o) {
    this.value = o.target.value;
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
          min=${g(this.min)}
          max=${g(this.max)}
          step=${g(this.step)}
          .value=${Lt(this.value)}
          placeholder=${g(this.placeholder)}
          autocomplete=${g(this.autocomplete)}
          pattern=${g(this.pattern)}
          minlength=${g(this.minlength)}
          maxlength=${g(this.maxlength)}
          inputmode=${g(this.inputmode)}
          ?required=${this.required}
          ?readonly=${this.readonly}
          ?disabled=${this.disabled}
          aria-label=${this.label ? p : this.accessibleName}
          aria-describedby=${g(this.noteId)}
          aria-invalid=${this.invalid ? "true" : "false"}
          @input=${this.handleInput}
          @change=${this.handleChange}
        />
        ${this.renderNote()}
      </div>
    `;
  }
};
w.styles = ce;
O([
  n()
], w.prototype, "type", 2);
O([
  n()
], w.prototype, "value", 2);
O([
  n()
], w.prototype, "placeholder", 2);
O([
  n()
], w.prototype, "autocomplete", 2);
O([
  n({ type: Boolean, reflect: !0 })
], w.prototype, "readonly", 2);
O([
  n()
], w.prototype, "min", 2);
O([
  n()
], w.prototype, "max", 2);
O([
  n()
], w.prototype, "step", 2);
O([
  n()
], w.prototype, "pattern", 2);
O([
  n()
], w.prototype, "minlength", 2);
O([
  n()
], w.prototype, "maxlength", 2);
O([
  n()
], w.prototype, "inputmode", 2);
O([
  n({ attribute: "fallback-label" })
], w.prototype, "fallbackLabel", 2);
w = O([
  h("vox-input")
], w);
var Tr = Object.getOwnPropertyDescriptor, Hr = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? Tr(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = i(t) || t);
  return t;
};
let Nt = class extends v {
  handleSlotChange(o) {
    const e = o.target.assignedElements();
    e.forEach((r, s) => {
      const t = s === 0 ? "start" : s === e.length - 1 ? "end" : "middle";
      r.setAttribute("data-vox-group", t);
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
Nt.styles = d`
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
Nt = Hr([
  h("vox-input-group")
], Nt);
var Ir = Object.defineProperty, Nr = Object.getOwnPropertyDescriptor, At = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? Nr(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && Ir(e, r, t), t;
};
let $e = class extends v {
  constructor() {
    super(...arguments), this.value = "", this.checked = !1, this.disabled = !1;
  }
  select() {
    this.disabled || this.dispatchEvent(
      new CustomEvent("vox-radio-select", { bubbles: !0, composed: !0 })
    );
  }
  handleKeydown(o) {
    (o.key === " " || o.key === "Enter") && (o.preventDefault(), this.select());
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
$e.styles = d`
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
At([
  n()
], $e.prototype, "value", 2);
At([
  n({ type: Boolean, reflect: !0 })
], $e.prototype, "checked", 2);
At([
  n({ type: Boolean, reflect: !0 })
], $e.prototype, "disabled", 2);
$e = At([
  h("vox-radio")
], $e);
var qr = Object.defineProperty, Br = Object.getOwnPropertyDescriptor, Xt = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? Br(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && qr(e, r, t), t;
};
let He = class extends m {
  constructor() {
    super(...arguments), this.value = "", this.requiredMessage = "Please select an option.";
  }
  get radios() {
    return [...this.querySelectorAll("vox-radio")];
  }
  formResetCallback() {
    this.value = "";
  }
  updated() {
    this.internals.setFormValue(this.value || null), this.invalid = this.required && !this.value, this.internals.setValidity(
      this.invalid ? { valueMissing: !0 } : {},
      this.requiredMessage,
      this
    ), this.syncRadios();
  }
  syncRadios() {
    const o = this.radios, e = o.some((s) => s.value === this.value && this.value !== ""), r = o.find((s) => !s.disabled);
    o.forEach((s) => {
      var a, i;
      s.checked = this.value !== "" && s.value === this.value;
      const t = e ? s.checked : s === r;
      (i = (a = s.shadowRoot) == null ? void 0 : a.querySelector(".radio")) == null || i.setAttribute("tabindex", t ? "0" : "-1");
    });
  }
  handleSelect(o) {
    const e = o.target;
    !(e instanceof HTMLElement) || e.tagName !== "VOX-RADIO" || (o.stopPropagation(), this.value !== e.value && (this.value = e.value, this.dispatchEvent(new Event("change", { bubbles: !0 }))));
  }
  handleKeydown(o) {
    var u, x;
    const r = {
      ArrowDown: 1,
      ArrowRight: 1,
      ArrowUp: -1,
      ArrowLeft: -1
    }[o.key];
    if (!r) return;
    o.preventDefault();
    const s = this.radios.filter((f) => !f.disabled);
    if (s.length === 0) return;
    const t = s.findIndex((f) => f.checked), a = (Math.max(t, 0) + r + s.length) % s.length, i = s[a];
    i.select(), (x = (u = i.shadowRoot) == null ? void 0 : u.querySelector(".radio")) == null || x.focus();
  }
  render() {
    return l`
      <div
        class="field"
        role="radiogroup"
        aria-label=${this.label || p}
        aria-describedby=${g(this.noteId)}
        aria-invalid=${this.invalid ? "true" : "false"}
        aria-required=${this.required ? "true" : "false"}
      >
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
He.styles = [
  ce,
  d`
      .options {
        display: flex;
        flex-direction: column;
        gap: var(--vox-space-2);
        margin-top: var(--vox-space-1);
      }
    `
];
Xt([
  n()
], He.prototype, "value", 2);
Xt([
  n({ attribute: "required-message" })
], He.prototype, "requiredMessage", 2);
He = Xt([
  h("vox-radio-group")
], He);
var Rr = Object.defineProperty, Ur = Object.getOwnPropertyDescriptor, T = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? Ur(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && Rr(e, r, t), t;
};
let L = class extends m {
  constructor() {
    super(...arguments), this.value = "", this.min = "0", this.max = "100", this.step = "1", this.showValue = !1, this.unit = "", this.fallbackLabel = "slider", this.defaultValue = "";
  }
  connectedCallback() {
    if (super.connectedCallback(), this.value === "") {
      const o = (Number(this.min) + Number(this.max)) / 2;
      this.value = String(Number.isFinite(o) ? o : this.min);
    }
    this.defaultValue = this.value;
  }
  formResetCallback() {
    this.value = this.defaultValue;
  }
  updated() {
    this.internals.setFormValue(this.value), this.inputEl && this.syncValidity(this.inputEl);
  }
  focus(o) {
    var e;
    (e = this.inputEl) == null || e.focus(o);
  }
  handleInput(o) {
    this.value = o.target.value;
  }
  handleChange() {
    this.dispatchEvent(new Event("change", { bubbles: !0 }));
  }
  render() {
    const o = this.unit ? `${this.value}${this.unit}` : this.value;
    return l`
      <div class="field">
        ${this.renderLabel("range")}
        <div class="row">
          <input
            id="range"
            class="control"
            type="range"
            min=${this.min}
            max=${this.max}
            step=${this.step}
            .value=${Lt(this.value)}
            ?disabled=${this.disabled}
            aria-label=${this.label ? p : this.fallbackName(this.fallbackLabel)}
            aria-valuetext=${g(this.unit ? o : void 0)}
            aria-describedby=${g(this.noteId)}
            aria-invalid=${this.invalid ? "true" : "false"}
            @input=${this.handleInput}
            @change=${this.handleChange}
          />
          ${this.showValue ? l`<span class="readout" aria-hidden="true">${o}</span>` : p}
        </div>
        ${this.renderNote()}
      </div>
    `;
  }
};
L.styles = [
  ce,
  d`
      .row {
        display: flex;
        align-items: center;
        gap: var(--vox-space-3);
      }

      .readout {
        flex: none;
        min-width: 3.5ch;
        font-size: 14px;
        font-variant-numeric: tabular-nums;
        color: var(--vox-color-text-2);
        text-align: right;
      }

      input.control {
        appearance: none;
        -webkit-appearance: none;
        padding: 0;
        border: none;
        background: transparent;
        cursor: pointer;
        /* Room for the thumb's focus ring at both ends of the track. */
        height: 24px;
      }

      input.control:focus {
        outline: none;
        border: none;
        box-shadow: none;
      }

      input.control:disabled {
        cursor: not-allowed;
      }

      /*
       * Track and thumb need vendor-prefixed selectors, and a browser drops
       * the whole rule if it doesn't recognise one — so no grouping here.
       */
      input.control::-webkit-slider-runnable-track {
        height: 6px;
        border-radius: var(--vox-radius-full);
        /* 3:1 against the page background, per WCAG 1.4.11. */
        background-color: var(--vox-color-border);
      }

      input.control::-moz-range-track {
        height: 6px;
        border-radius: var(--vox-radius-full);
        background-color: var(--vox-color-border);
      }

      input.control::-webkit-slider-thumb {
        appearance: none;
        -webkit-appearance: none;
        width: 20px;
        height: 20px;
        margin-top: -7px;
        border: 2px solid var(--vox-color-bg);
        border-radius: var(--vox-radius-full);
        background-color: var(--vox-color-brand-1);
        transition: box-shadow var(--vox-transition-fast);
      }

      input.control::-moz-range-thumb {
        width: 20px;
        height: 20px;
        border: 2px solid var(--vox-color-bg);
        border-radius: var(--vox-radius-full);
        background-color: var(--vox-color-brand-1);
        transition: box-shadow var(--vox-transition-fast);
      }

      input.control:focus-visible::-webkit-slider-thumb {
        box-shadow: 0 0 0 3px var(--vox-color-brand-soft);
      }

      input.control:focus-visible::-moz-range-thumb {
        box-shadow: 0 0 0 3px var(--vox-color-brand-soft);
      }

      @media (prefers-reduced-motion: reduce) {
        input.control::-webkit-slider-thumb,
        input.control::-moz-range-thumb {
          transition: none;
        }
      }
    `
];
T([
  n()
], L.prototype, "value", 2);
T([
  n()
], L.prototype, "min", 2);
T([
  n()
], L.prototype, "max", 2);
T([
  n()
], L.prototype, "step", 2);
T([
  n({ type: Boolean, attribute: "show-value" })
], L.prototype, "showValue", 2);
T([
  n()
], L.prototype, "unit", 2);
T([
  n({ attribute: "fallback-label" })
], L.prototype, "fallbackLabel", 2);
T([
  ke("input")
], L.prototype, "inputEl", 2);
L = T([
  h("vox-range")
], L);
var Fr = Object.defineProperty, Zr = Object.getOwnPropertyDescriptor, de = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? Zr(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && Fr(e, r, t), t;
};
let V = class extends m {
  constructor() {
    super(...arguments), this.value = "", this.multiple = !1, this.values = [], this.fallbackLabel = "options";
  }
  formResetCallback() {
    this.value = "", this.values = [], this.syncOptions();
  }
  updated() {
    this.internals.setFormValue(this.multiple ? this.formData() : this.value), this.selectEl && this.syncValidity(this.selectEl);
  }
  focus(o) {
    var e;
    (e = this.selectEl) == null || e.focus(o);
  }
  /**
   * A multi-select submits one entry per selection, which only FormData can
   * express. Without a `name` there is nothing to key them on, so submit
   * nothing — matching a native select with no name.
   */
  formData() {
    if (!this.name) return null;
    const o = new FormData();
    for (const e of this.values) o.append(this.name, e);
    return o;
  }
  syncOptions() {
    if (!this.selectEl) return;
    const o = this.renderRoot.querySelector("slot");
    if (o) {
      if (this.selectEl.replaceChildren(
        ...o.assignedElements().filter((e) => e instanceof HTMLOptionElement || e instanceof HTMLOptGroupElement).map((e) => e.cloneNode(!0))
      ), this.multiple) {
        const e = new Set(this.values);
        for (const r of this.selectEl.options)
          r.selected = e.has(r.value);
        this.values = this.selected();
        return;
      }
      this.value && (this.selectEl.value = this.value), this.value = this.selectEl.value;
    }
  }
  selected() {
    return [...this.selectEl.selectedOptions].map((o) => o.value);
  }
  handleChange() {
    this.multiple ? (this.values = this.selected(), this.value = this.values[0] ?? "") : this.value = this.selectEl.value, this.dispatchEvent(new Event("change", { bubbles: !0 }));
  }
  render() {
    return l`
      <div class="field">
        ${this.renderLabel("select")}
        <select
          id="select"
          class="control"
          ?multiple=${this.multiple}
          size=${g(this.multiple ? this.size ?? 4 : void 0)}
          ?required=${this.required}
          ?disabled=${this.disabled}
          aria-label=${this.label ? p : this.fallbackName(this.fallbackLabel)}
          aria-describedby=${g(this.noteId)}
          aria-invalid=${this.invalid ? "true" : "false"}
          @change=${this.handleChange}
        ></select>
        ${this.renderNote()}
      </div>
      <div hidden><slot @slotchange=${this.syncOptions}></slot></div>
    `;
  }
};
V.styles = [
  ce,
  d`
      select.control {
        appearance: none;
        padding-right: var(--vox-space-8);
        background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='16' height='16' viewBox='0 0 24 24' fill='none' stroke='%23808080' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'%3E%3Cpath d='m6 9 6 6 6-6'/%3E%3C/svg%3E");
        background-repeat: no-repeat;
        background-position: right var(--vox-space-3) center;
        cursor: pointer;
      }

      /* A list box has no collapsed affordance, so drop the chevron. */
      :host([multiple]) select.control {
        appearance: none;
        padding-right: var(--vox-space-3);
        background-image: none;
        cursor: default;
      }

      :host([multiple]) select.control option {
        padding: var(--vox-space-1) var(--vox-space-2);
      }
    `
];
de([
  n()
], V.prototype, "value", 2);
de([
  n({ type: Boolean, reflect: !0 })
], V.prototype, "multiple", 2);
de([
  n({ type: Number })
], V.prototype, "size", 2);
de([
  n({ type: Array })
], V.prototype, "values", 2);
de([
  n({ attribute: "fallback-label" })
], V.prototype, "fallbackLabel", 2);
de([
  ke("select")
], V.prototype, "selectEl", 2);
V = de([
  h("vox-select")
], V);
var Kr = Object.defineProperty, Wr = Object.getOwnPropertyDescriptor, Qt = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? Wr(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && Kr(e, r, t), t;
};
let Ie = class extends m {
  constructor() {
    super(...arguments), this.checked = !1, this.value = "on";
  }
  formResetCallback() {
    this.checked = !1;
  }
  updated() {
    this.internals.setFormValue(this.checked ? this.value : null);
  }
  handleChange(o) {
    this.checked = o.target.checked, this.dispatchEvent(new Event("change", { bubbles: !0 }));
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
Ie.styles = [
  Io,
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
Qt([
  n({ type: Boolean, reflect: !0 })
], Ie.prototype, "checked", 2);
Qt([
  n()
], Ie.prototype, "value", 2);
Ie = Qt([
  h("vox-switch")
], Ie);
var Gr = Object.defineProperty, Yr = Object.getOwnPropertyDescriptor, Me = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? Yr(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && Gr(e, r, t), t;
};
let B = class extends m {
  constructor() {
    super(...arguments), this.value = "", this.rows = 4, this.readonly = !1, this.fallbackLabel = "text area";
  }
  formResetCallback() {
    this.value = "";
  }
  updated() {
    this.internals.setFormValue(this.value);
    const o = this.renderRoot.querySelector("textarea");
    o && this.syncValidity(o);
  }
  focus(o) {
    var e;
    (e = this.renderRoot.querySelector("textarea")) == null || e.focus(o);
  }
  handleInput(o) {
    this.value = o.target.value;
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
          .value=${Lt(this.value)}
          placeholder=${g(this.placeholder)}
          ?required=${this.required}
          ?readonly=${this.readonly}
          ?disabled=${this.disabled}
          aria-label=${this.label ? p : this.fallbackName(this.fallbackLabel)}
          aria-describedby=${g(this.noteId)}
          aria-invalid=${this.invalid ? "true" : "false"}
          @input=${this.handleInput}
          @change=${this.handleChange}
        ></textarea>
        ${this.renderNote()}
      </div>
    `;
  }
};
B.styles = [
  ce,
  d`
      textarea.control {
        resize: vertical;
        min-height: 4em;
      }
    `
];
Me([
  n()
], B.prototype, "value", 2);
Me([
  n()
], B.prototype, "placeholder", 2);
Me([
  n({ type: Number })
], B.prototype, "rows", 2);
Me([
  n({ type: Boolean, reflect: !0 })
], B.prototype, "readonly", 2);
Me([
  n({ attribute: "fallback-label" })
], B.prototype, "fallbackLabel", 2);
B = Me([
  h("vox-textarea")
], B);
var Jr = Object.defineProperty, Xr = Object.getOwnPropertyDescriptor, tt = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? Xr(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && Jr(e, r, t), t;
};
let te = class extends v {
  constructor() {
    super(...arguments), this.alt = "", this.initials = "", this.size = "md";
  }
  render() {
    const o = !this.src && this.alt;
    return l`
      <span
        class="avatar"
        role=${o ? "img" : p}
        aria-label=${o ? this.alt : p}
      >
        ${this.src ? l`<img src=${this.src} alt=${this.alt} />` : this.initials}
      </span>
    `;
  }
};
te.styles = d`
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
tt([
  n()
], te.prototype, "src", 2);
tt([
  n()
], te.prototype, "alt", 2);
tt([
  n()
], te.prototype, "initials", 2);
tt([
  n({ reflect: !0 })
], te.prototype, "size", 2);
te = tt([
  h("vox-avatar")
], te);
var Qr = Object.defineProperty, es = Object.getOwnPropertyDescriptor, eo = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? es(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && Qr(e, r, t), t;
};
let Ne = class extends v {
  constructor() {
    super(...arguments), this.heading = "", this.reverse = !1, this.hasActions = !1;
  }
  handleActionsSlotChange(o) {
    const e = o.target;
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
Ne.styles = d`
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
eo([
  n()
], Ne.prototype, "heading", 2);
eo([
  n({ type: Boolean, reflect: !0 })
], Ne.prototype, "reverse", 2);
Ne = eo([
  h("vox-billboard")
], Ne);
var ts = Object.defineProperty, os = Object.getOwnPropertyDescriptor, No = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? os(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && ts(e, r, t), t;
};
let pt = class extends v {
  constructor() {
    super(...arguments), this.label = "Breadcrumbs";
  }
  render() {
    return l`
      <nav aria-label=${this.label}>
        <slot></slot>
      </nav>
    `;
  }
};
pt.styles = d`
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
No([
  n()
], pt.prototype, "label", 2);
pt = No([
  h("vox-breadcrumbs")
], pt);
var rs = Object.defineProperty, ss = Object.getOwnPropertyDescriptor, he = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? ss(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && rs(e, r, t), t;
};
let D = class extends v {
  constructor() {
    super(...arguments), this.siteTitle = "", this.href = "/", this.navLabel = "Main", this.menuLabel = "Open menu", this.closeMenuLabel = "Close menu", this.mobileOpen = !1, this.handleOutsideClick = (o) => {
      this.mobileOpen && !o.composedPath().includes(this) && (this.mobileOpen = !1);
    }, this.handleFocusOut = (o) => {
      const e = o.relatedTarget;
      this.mobileOpen && !(e && this.contains(e)) && (this.mobileOpen = !1);
    }, this.handleKeydown = (o) => {
      var e;
      o.key === "Escape" && this.mobileOpen && (this.mobileOpen = !1, (e = this.renderRoot.querySelector(".menu-toggle")) == null || e.focus());
    }, this.toggleMobileMenu = () => {
      this.mobileOpen = !this.mobileOpen;
    }, this.closeMobileMenu = () => {
      this.mobileOpen = !1;
    };
  }
  connectedCallback() {
    super.connectedCallback(), document.addEventListener("click", this.handleOutsideClick), this.addEventListener("keydown", this.handleKeydown), this.addEventListener("focusout", this.handleFocusOut);
  }
  disconnectedCallback() {
    super.disconnectedCallback(), document.removeEventListener("click", this.handleOutsideClick), this.removeEventListener("keydown", this.handleKeydown), this.removeEventListener("focusout", this.handleFocusOut);
  }
  render() {
    return l`
      <header class="header">
        <a class="brand" href=${this.href}>
          <slot name="logo"></slot>
          ${this.siteTitle ? l`<span>${this.siteTitle}</span>` : p}
        </a>
        <button
          type="button"
          class="menu-toggle"
          aria-expanded=${this.mobileOpen ? "true" : "false"}
          aria-controls="nav-wrap"
          aria-label=${this.mobileOpen ? this.closeMenuLabel : this.menuLabel}
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
            ${this.mobileOpen ? q.close : q.menu}
          </svg>
        </button>
        <div id="nav-wrap" class="nav-wrap${this.mobileOpen ? " open" : ""}">
          <nav aria-label=${this.navLabel}>
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
D.styles = d`
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
      margin-inline-start: auto;
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
he([
  n({ attribute: "site-title" })
], D.prototype, "siteTitle", 2);
he([
  n()
], D.prototype, "href", 2);
he([
  n({ attribute: "nav-label" })
], D.prototype, "navLabel", 2);
he([
  n({ attribute: "menu-label" })
], D.prototype, "menuLabel", 2);
he([
  n({ attribute: "close-menu-label" })
], D.prototype, "closeMenuLabel", 2);
he([
  A()
], D.prototype, "mobileOpen", 2);
D = he([
  h("vox-header")
], D);
var as = Object.defineProperty, is = Object.getOwnPropertyDescriptor, K = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? is(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && as(e, r, t), t;
};
let E = class extends v {
  constructor() {
    super(...arguments), this.previousLabel = "", this.nextLabel = "", this.label = "Series", this.previousText = "Previous", this.nextText = "Next";
  }
  render() {
    return l`
      <nav aria-label=${this.label}>
        ${this.previousHref ? l`
              <a href=${this.previousHref} rel="prev">
                <span class="direction"
                  ><span aria-hidden="true">←</span> ${this.previousText}</span
                >
                <span class="title">${this.previousLabel}</span>
              </a>
            ` : p}
        ${this.nextHref ? l`
              <a class="next" href=${this.nextHref} rel="next">
                <span class="direction"
                  >${this.nextText} <span aria-hidden="true">→</span></span
                >
                <span class="title">${this.nextLabel}</span>
              </a>
            ` : p}
      </nav>
    `;
  }
};
E.styles = d`
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
      margin-inline-start: auto;
      text-align: end;
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
K([
  n({ attribute: "previous-href" })
], E.prototype, "previousHref", 2);
K([
  n({ attribute: "previous-label" })
], E.prototype, "previousLabel", 2);
K([
  n({ attribute: "next-href" })
], E.prototype, "nextHref", 2);
K([
  n({ attribute: "next-label" })
], E.prototype, "nextLabel", 2);
K([
  n()
], E.prototype, "label", 2);
K([
  n({ attribute: "previous-text" })
], E.prototype, "previousText", 2);
K([
  n({ attribute: "next-text" })
], E.prototype, "nextText", 2);
E = K([
  h("vox-series-nav")
], E);
var ns = Object.defineProperty, ls = Object.getOwnPropertyDescriptor, S = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? ls(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && ns(e, r, t), t;
};
let we = class extends v {
  constructor() {
    super(...arguments), this.label = "Section", this.toggleLabel = "Menu", this.mobileOpen = !1, this.handleOutsideClick = (o) => {
      this.mobileOpen && !o.composedPath().includes(this) && (this.mobileOpen = !1);
    }, this.handleFocusOut = (o) => {
      const e = o.relatedTarget;
      this.mobileOpen && !(e && this.contains(e)) && (this.mobileOpen = !1);
    }, this.handleKeydown = (o) => {
      var e;
      o.key === "Escape" && this.mobileOpen && (this.mobileOpen = !1, (e = this.renderRoot.querySelector(".toggle")) == null || e.focus());
    }, this.toggleMobile = () => {
      this.mobileOpen = !this.mobileOpen;
    }, this.handleNavClick = (o) => {
      o.composedPath().some((e) => e instanceof HTMLAnchorElement) && (this.mobileOpen = !1);
    };
  }
  connectedCallback() {
    super.connectedCallback(), document.addEventListener("click", this.handleOutsideClick), this.addEventListener("keydown", this.handleKeydown), this.addEventListener("focusout", this.handleFocusOut);
  }
  disconnectedCallback() {
    super.disconnectedCallback(), document.removeEventListener("click", this.handleOutsideClick), this.removeEventListener("keydown", this.handleKeydown), this.removeEventListener("focusout", this.handleFocusOut);
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
          ${this.mobileOpen ? q.close : q.menu}
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
we.styles = d`
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
S([
  n()
], we.prototype, "label", 2);
S([
  n({ attribute: "toggle-label" })
], we.prototype, "toggleLabel", 2);
S([
  A()
], we.prototype, "mobileOpen", 2);
we = S([
  h("vox-sidenav")
], we);
let oe = class extends v {
  constructor() {
    super(...arguments), this.heading = "", this.open = !1, this.itemsId = `vox-sidenav-group-items-${oe.nextId++}`;
  }
  toggle() {
    this.open = !this.open;
  }
  render() {
    return l`
      <button
        class="trigger"
        aria-expanded=${this.open ? "true" : "false"}
        aria-controls=${this.itemsId}
        @click=${this.toggle}
      >
        ${this.heading}
        <svg class="chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="m9 6 6 6-6 6" />
        </svg>
      </button>
      <div id=${this.itemsId} class="items"><slot></slot></div>
    `;
  }
};
oe.nextId = 0;
oe.styles = d`
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
S([
  n()
], oe.prototype, "heading", 2);
S([
  n({ type: Boolean, reflect: !0 })
], oe.prototype, "open", 2);
oe = S([
  h("vox-sidenav-group")
], oe);
let qe = class extends v {
  constructor() {
    super(...arguments), this.href = "#", this.current = !1, this.hasIcon = !1;
  }
  handleIconSlotChange(o) {
    const e = o.target;
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
qe.styles = d`
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
S([
  n()
], qe.prototype, "href", 2);
S([
  n({ type: Boolean, reflect: !0 })
], qe.prototype, "current", 2);
qe = S([
  h("vox-sidenav-item")
], qe);
var cs = Object.defineProperty, ds = Object.getOwnPropertyDescriptor, qo = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? ds(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && cs(e, r, t), t;
};
let vt = class extends v {
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
vt.styles = d`
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
qo([
  n()
], vt.prototype, "label", 2);
vt = qo([
  h("vox-subnav")
], vt);
var hs = Object.defineProperty, ps = Object.getOwnPropertyDescriptor, pe = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? ps(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && hs(e, r, t), t;
};
let qt = class extends v {
  get tabs() {
    return [...this.querySelectorAll("vox-tab")];
  }
  get panels() {
    return [...this.querySelectorAll("vox-tab-panel")];
  }
  sync() {
    const o = this.tabs;
    o.length > 0 && !o.some((r) => r.selected) && (o[0].selected = !0);
    const e = o.find((r) => r.selected);
    this.panels.forEach((r) => {
      r.active = r.name === (e == null ? void 0 : e.panel);
    });
  }
  handleSelect(o) {
    const e = o.target;
    e.tagName === "VOX-TAB" && (this.tabs.forEach((r) => r.selected = r === e), this.sync(), this.dispatchEvent(
      new CustomEvent("vox-tab-change", {
        detail: { panel: e.panel },
        bubbles: !0,
        composed: !0
      })
    ));
  }
  handleKeydown(o) {
    const e = this.tabs;
    if (o.key === "Home" || o.key === "End") {
      o.preventDefault();
      const i = e[o.key === "Home" ? 0 : e.length - 1];
      i.select(), i.focusTab();
      return;
    }
    const s = { ArrowRight: 1, ArrowLeft: -1 }[o.key];
    if (!s) return;
    o.preventDefault();
    const t = e.findIndex((i) => i.selected), a = e[(t + s + e.length) % e.length];
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
qt.styles = d`
    :host {
      display: block;
    }

    .tablist {
      display: flex;
      gap: var(--vox-space-1);
      border-bottom: 1px solid var(--vox-color-divider);
    }
  `;
qt = pe([
  h("vox-tabs")
], qt);
let Be = class extends v {
  constructor() {
    super(...arguments), this.panel = "", this.selected = !1;
  }
  select() {
    this.dispatchEvent(
      new CustomEvent("vox-tab-select", { bubbles: !0, composed: !0 })
    );
  }
  focusTab() {
    var o;
    (o = this.renderRoot.querySelector(".tab")) == null || o.focus();
  }
  render() {
    return l`
      <button
        id="tab-${this.panel}"
        class="tab"
        role="tab"
        aria-selected=${this.selected ? "true" : "false"}
        aria-controls="panel-${this.panel}"
        tabindex=${this.selected ? "0" : "-1"}
        @click=${this.select}
      >
        <slot></slot>
      </button>
    `;
  }
};
Be.styles = d`
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
pe([
  n()
], Be.prototype, "panel", 2);
pe([
  n({ type: Boolean, reflect: !0 })
], Be.prototype, "selected", 2);
Be = pe([
  h("vox-tab")
], Be);
let Re = class extends v {
  constructor() {
    super(...arguments), this.name = "", this.active = !1;
  }
  render() {
    return l`
      <div
        id="panel-${this.name}"
        role="tabpanel"
        aria-labelledby="tab-${this.name}"
        tabindex="0"
      >
        <slot></slot>
      </div>
    `;
  }
};
Re.styles = d`
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

    div:focus-visible {
      outline: 2px solid var(--vox-color-brand-1);
      outline-offset: 2px;
      border-radius: var(--vox-radius-sm);
    }
  `;
pe([
  n()
], Re.prototype, "name", 2);
pe([
  n({ type: Boolean, reflect: !0 })
], Re.prototype, "active", 2);
Re = pe([
  h("vox-tab-panel")
], Re);
var vs = Object.defineProperty, us = Object.getOwnPropertyDescriptor, ot = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? us(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && vs(e, r, t), t;
};
let ut = class extends v {
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
ut.styles = d`
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
ot([
  n()
], ut.prototype, "heading", 2);
ut = ot([
  h("vox-toc")
], ut);
let Ue = class extends v {
  constructor() {
    super(...arguments), this.href = "#", this.current = !1, this.hasChildren = !1;
  }
  handleChildrenSlotChange(o) {
    const e = o.target;
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
Ue.styles = d`
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
ot([
  n()
], Ue.prototype, "href", 2);
ot([
  n({ type: Boolean, reflect: !0 })
], Ue.prototype, "current", 2);
Ue = ot([
  h("vox-toc-item")
], Ue);
var xs = Object.defineProperty, fs = Object.getOwnPropertyDescriptor, Le = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? fs(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && xs(e, r, t), t;
};
let R = class extends v {
  constructor() {
    super(...arguments), this.heading = "", this.open = !1, this.closeLabel = "Close dialog", this.lightDismiss = !1, this.hasFooter = !1;
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
  handleClick(o) {
    this.lightDismiss && o.target === this.dialogEl && this.close();
  }
  handleFooterSlotChange(o) {
    const e = o.target;
    this.hasFooter = e.assignedNodes({ flatten: !0 }).length > 0, this.requestUpdate();
  }
  render() {
    return l`
      <dialog
        aria-labelledby=${this.heading ? "heading" : p}
        @close=${this.handleNativeClose}
        @click=${this.handleClick}
      >
        <div class="header">
          <h2 class="heading" id="heading">${this.heading}</h2>
          <button class="close" aria-label=${this.closeLabel} @click=${this.close}>
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
R.styles = d`
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
Le([
  n()
], R.prototype, "heading", 2);
Le([
  n({ type: Boolean })
], R.prototype, "open", 2);
Le([
  n({ attribute: "close-label" })
], R.prototype, "closeLabel", 2);
Le([
  n({ type: Boolean, attribute: "light-dismiss" })
], R.prototype, "lightDismiss", 2);
Le([
  ke("dialog")
], R.prototype, "dialogEl", 2);
R = Le([
  h("vox-dialog")
], R);
var bs = Object.defineProperty, gs = Object.getOwnPropertyDescriptor, to = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? gs(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && bs(e, r, t), t;
};
let re = class extends v {
  constructor() {
    super(...arguments), this.summary = "Show details", this.open = !1, this.panelId = `vox-disclosure-panel-${re.nextId++}`;
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
        aria-controls=${this.panelId}
        @click=${this.toggle}
      >
        <svg class="chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="m9 6 6 6-6 6" />
        </svg>
        ${this.summary}
      </button>
      <div id=${this.panelId} class="panel" ?hidden=${!this.open}>
        <slot></slot>
      </div>
    `;
  }
};
re.nextId = 0;
re.styles = d`
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
to([
  n()
], re.prototype, "summary", 2);
to([
  n({ type: Boolean, reflect: !0 })
], re.prototype, "open", 2);
re = to([
  h("vox-disclosure")
], re);
var ms = Object.defineProperty, ys = Object.getOwnPropertyDescriptor, oo = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? ys(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && ms(e, r, t), t;
};
let Fe = class extends v {
  constructor() {
    super(...arguments), this.label = "Menu", this.open = !1, this.handleOutsideClick = (o) => {
      this.open && !o.composedPath().includes(this) && (this.open = !1);
    }, this.handleFocusOut = (o) => {
      const e = o.relatedTarget;
      this.open && !(e && this.contains(e)) && (this.open = !1);
    }, this.handleKeydown = (o) => {
      var e;
      if (o.key === "Escape" && this.open) {
        this.open = !1, (e = this.renderRoot.querySelector(".trigger")) == null || e.focus();
        return;
      }
      if (o.key === "ArrowDown" || o.key === "ArrowUp") {
        o.preventDefault();
        const r = o.key === "ArrowDown" ? 1 : -1;
        if (!this.open) {
          this.open = !0, this.updateComplete.then(
            () => this.focusItem(o.key === "ArrowDown" ? 0 : -1)
          );
          return;
        }
        const t = [...this.querySelectorAll("a, button")].indexOf(document.activeElement);
        this.focusItem(Math.max(t, 0) + r);
      }
    };
  }
  connectedCallback() {
    super.connectedCallback(), document.addEventListener("click", this.handleOutsideClick), this.addEventListener("keydown", this.handleKeydown), this.addEventListener("focusout", this.handleFocusOut);
  }
  disconnectedCallback() {
    super.disconnectedCallback(), document.removeEventListener("click", this.handleOutsideClick), this.removeEventListener("keydown", this.handleKeydown), this.removeEventListener("focusout", this.handleFocusOut);
  }
  focusItem(o) {
    const e = [...this.querySelectorAll("a, button")];
    e.length !== 0 && e[(o + e.length) % e.length].focus();
  }
  toggle() {
    this.open = !this.open, this.open && this.updateComplete.then(() => this.focusItem(0));
  }
  render() {
    return l`
      <button
        class="trigger"
        aria-expanded=${this.open ? "true" : "false"}
        aria-haspopup="true"
        aria-controls="menu"
        @click=${this.toggle}
      >
        ${this.label}
        <svg class="chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="m6 9 6 6 6-6" />
        </svg>
      </button>
      <div id="menu" class="menu">
        <slot @click=${() => this.open = !1}></slot>
      </div>
    `;
  }
};
Fe.styles = d`
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
oo([
  n()
], Fe.prototype, "label", 2);
oo([
  n({ type: Boolean, reflect: !0 })
], Fe.prototype, "open", 2);
Fe = oo([
  h("vox-dropdown")
], Fe);
var $s = Object.defineProperty, ws = Object.getOwnPropertyDescriptor, Pt = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? ws(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && $s(e, r, t), t;
};
let _e = class extends v {
  constructor() {
    super(...arguments), this.placement = "bottom-end", this.open = !1, this.handleOutsideClick = (o) => {
      this.open && !o.composedPath().includes(this) && this.close();
    }, this.handleKeydown = (o) => {
      var e;
      if (o.key === "Escape" && this.open) {
        this.close(), (e = this.renderRoot.querySelector(".trigger")) == null || e.focus();
        return;
      }
      if ((o.key === "ArrowDown" || o.key === "ArrowUp") && this.open) {
        o.preventDefault();
        const r = [...this.querySelectorAll("a, button")].filter(
          (u) => u.slot !== "trigger"
        );
        if (r.length === 0) return;
        const s = document.activeElement, t = r.indexOf(s), a = o.key === "ArrowDown" ? 1 : -1;
        r[(Math.max(t, 0) + a + r.length) % r.length].focus();
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
        aria-label=${this.label ?? p}
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
_e.styles = d`
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
Pt([
  n()
], _e.prototype, "label", 2);
Pt([
  n({ reflect: !0 })
], _e.prototype, "placement", 2);
Pt([
  n({ type: Boolean, reflect: !0 })
], _e.prototype, "open", 2);
_e = Pt([
  h("vox-menu")
], _e);
var _s = Object.defineProperty, Cs = Object.getOwnPropertyDescriptor, rt = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? Cs(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && _s(e, r, t), t;
};
let xt = class extends v {
  constructor() {
    super(...arguments), this.single = !1;
  }
  handleToggle(o) {
    if (!this.single) return;
    const e = o.target;
    e.open && this.querySelectorAll("vox-accordion-item").forEach(
      (r) => {
        r !== e && (r.open = !1);
      }
    );
  }
  render() {
    return l`<slot @vox-toggle=${this.handleToggle}></slot>`;
  }
};
xt.styles = d`
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
rt([
  n({ type: Boolean })
], xt.prototype, "single", 2);
xt = rt([
  h("vox-accordion")
], xt);
let se = class extends v {
  constructor() {
    super(...arguments), this.heading = "", this.open = !1, this.panelId = `vox-accordion-panel-${se.nextId++}`;
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
          aria-controls=${this.panelId}
          @click=${this.toggle}
        >
          ${this.heading}
          <svg class="chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d="m6 9 6 6 6-6" />
          </svg>
        </button>
      </h3>
      <div id=${this.panelId} class="panel" ?hidden=${!this.open}>
        <slot></slot>
      </div>
    `;
  }
};
se.nextId = 0;
se.styles = d`
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
rt([
  n()
], se.prototype, "heading", 2);
rt([
  n({ type: Boolean, reflect: !0 })
], se.prototype, "open", 2);
se = rt([
  h("vox-accordion-item")
], se);
var Os = Object.defineProperty, ks = Object.getOwnPropertyDescriptor, Ae = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? ks(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && Os(e, r, t), t;
};
const Ms = {
  info: "info",
  success: "check-circle",
  warning: "warning",
  danger: "x-circle"
};
let U = class extends v {
  constructor() {
    super(...arguments), this.variant = "info", this.heading = "", this.dismissible = !1, this.open = !0, this.dismissLabel = "Dismiss";
  }
  dismiss() {
    this.open = !1, this.dispatchEvent(
      new CustomEvent("vox-dismiss", { bubbles: !0, composed: !0 })
    );
  }
  render() {
    const o = this.variant === "danger" || this.variant === "warning";
    return l`
      <div
        class="alert ${this.variant}"
        role=${o ? "alert" : "status"}
      >
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
          ${q[Ms[this.variant]]}
        </svg>
        <div class="body">
          ${this.heading ? l`<p class="heading">${this.heading}</p>` : p}
          <slot></slot>
        </div>
        ${this.dismissible ? l`
              <button class="close" aria-label=${this.dismissLabel} @click=${this.dismiss}>
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" aria-hidden="true">
                  <path d="M18 6 6 18M6 6l12 12" />
                </svg>
              </button>
            ` : p}
      </div>
    `;
  }
};
U.styles = d`
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
Ae([
  n()
], U.prototype, "variant", 2);
Ae([
  n()
], U.prototype, "heading", 2);
Ae([
  n({ type: Boolean })
], U.prototype, "dismissible", 2);
Ae([
  n({ type: Boolean, reflect: !0 })
], U.prototype, "open", 2);
Ae([
  n({ attribute: "dismiss-label" })
], U.prototype, "dismissLabel", 2);
U = Ae([
  h("vox-alert")
], U);
var Ls = Object.defineProperty, As = Object.getOwnPropertyDescriptor, Bo = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? As(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && Ls(e, r, t), t;
};
let ft = class extends v {
  constructor() {
    super(...arguments), this.variant = "brand";
  }
  render() {
    return l`<span class="badge ${this.variant}"><slot></slot></span>`;
  }
};
ft.styles = d`
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
Bo([
  n()
], ft.prototype, "variant", 2);
ft = Bo([
  h("vox-badge")
], ft);
var Ps = Object.defineProperty, Es = Object.getOwnPropertyDescriptor, ro = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? Es(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && Ps(e, r, t), t;
};
let Ze = class extends v {
  constructor() {
    super(...arguments), this.date = "";
  }
  render() {
    const o = /* @__PURE__ */ new Date(`${this.date}T00:00:00`), e = !Number.isNaN(o.getTime()), r = e ? o.toLocaleString(this.locale, { month: "short" }) : "—", s = e ? o.getDate() : "–";
    return l`
      <time class="tile" datetime=${this.date}>
        <span class="month">${r}</span>
        <span class="day">${s}</span>
      </time>
    `;
  }
};
Ze.styles = d`
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
ro([
  n()
], Ze.prototype, "date", 2);
ro([
  n()
], Ze.prototype, "locale", 2);
Ze = ro([
  h("vox-calendar-tile")
], Ze);
var Ss = Object.defineProperty, zs = Object.getOwnPropertyDescriptor, so = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? zs(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && Ss(e, r, t), t;
};
const js = {
  info: "Info",
  tip: "Tip",
  warning: "Warning",
  danger: "Danger"
}, Vs = {
  info: "info",
  tip: "check-circle",
  warning: "warning",
  danger: "x-circle"
};
let Ke = class extends v {
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
          ${q[Vs[this.variant]]}
        </svg>
        <div class="content">
          <p class="heading">${this.heading ?? js[this.variant]}</p>
          <slot></slot>
        </div>
      </div>
    `;
  }
};
Ke.styles = d`
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
so([
  n()
], Ke.prototype, "variant", 2);
so([
  n()
], Ke.prototype, "heading", 2);
Ke = so([
  h("vox-callout")
], Ke);
var Ds = Object.defineProperty, Ts = Object.getOwnPropertyDescriptor, Et = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? Ts(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && Ds(e, r, t), t;
};
let Ce = class extends v {
  constructor() {
    super(...arguments), this.heading = "", this.hasIcon = !1, this.hasBadge = !1, this.hasFooter = !1;
  }
  handleIconSlotChange(o) {
    const e = o.target;
    this.hasIcon = e.assignedNodes({ flatten: !0 }).length > 0, this.requestUpdate();
  }
  handleBadgeSlotChange(o) {
    const e = o.target;
    this.hasBadge = e.assignedNodes({ flatten: !0 }).length > 0, this.requestUpdate();
  }
  handleFooterSlotChange(o) {
    const e = o.target;
    this.hasFooter = e.assignedNodes({ flatten: !0 }).length > 0, this.requestUpdate();
  }
  render() {
    const o = l`
      <div class="badge ${this.hasBadge ? "has-badge" : ""}">
        <slot name="badge" @slotchange=${this.handleBadgeSlotChange}></slot>
      </div>
      <div class="icon ${this.hasIcon ? "has-icon" : ""}" aria-hidden="true">
        <slot name="icon" @slotchange=${this.handleIconSlotChange}></slot>
      </div>
      <h3 class="heading">${this.heading}</h3>
      <div class="body"><slot></slot></div>
      <div class="footer ${this.hasFooter ? "has-footer" : ""}">
        <slot name="footer" @slotchange=${this.handleFooterSlotChange}></slot>
      </div>
    `;
    return this.href !== void 0 ? l`<a class="card" href=${this.href} target=${this.target ?? p}>${o}</a>` : l`<div class="card">${o}</div>`;
  }
};
Ce.styles = d`
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
Et([
  n()
], Ce.prototype, "heading", 2);
Et([
  n()
], Ce.prototype, "href", 2);
Et([
  n()
], Ce.prototype, "target", 2);
Ce = Et([
  h("vox-card")
], Ce);
function Ro(o, e) {
  let r = e;
  for (; r < o.length && (o[r] === " " || o[r] === "	"); ) r++;
  return r;
}
function ve(o, e) {
  return {
    re: o,
    type: (r, s, t) => e.has(r) ? "keyword" : s[Ro(s, t)] === "(" ? "function" : "plain"
  };
}
function Bt(o) {
  return {
    re: o,
    type: (e, r, s) => r[Ro(r, s)] === ":" ? "property" : "string"
  };
}
const st = /-?\b\d+\.?\d*(?:[eE][+-]?\d+)?\b/y, W = /"(?:\\.|[^"\\])*"/y, ue = /'(?:\\.|[^'\\])*'/y, St = /#.*/y, Hs = /\/\/.*/y, Uo = /\/\*[\s\S]*?\*\//y;
function Co(o, e, r) {
  const s = o[o.length - 1];
  s && s.type === e ? s.text += r : o.push({ type: e, text: r });
}
function Is(o, e) {
  const r = [];
  let s = 0;
  for (; s < o.length; ) {
    let t = !1;
    for (const a of e) {
      a.re.lastIndex = s;
      const i = a.re.exec(o);
      if (i && i.index === s && i[0].length > 0) {
        const u = i[0], x = typeof a.type == "function" ? a.type(u, o, s + u.length) : a.type;
        Co(r, x, u), s += u.length, t = !0;
        break;
      }
    }
    t || (Co(r, "plain", o[s]), s++);
  }
  return r;
}
const Ns = /* @__PURE__ */ new Set([
  "if",
  "then",
  "elif",
  "else",
  "fi",
  "for",
  "while",
  "until",
  "do",
  "done",
  "case",
  "esac",
  "in",
  "function",
  "select",
  "time",
  "return",
  "exit",
  "break",
  "continue",
  "local",
  "export",
  "readonly",
  "declare",
  "unset",
  "shift",
  "eval",
  "exec",
  "trap",
  "set",
  "source",
  "alias",
  "unalias",
  "true",
  "false"
]), nt = [
  { re: St, type: "comment" },
  { re: /\$\{[^}]*\}|\$[A-Za-z_]\w*|\$[0-9@#?$!*_-]/y, type: "function" },
  { re: W, type: "string" },
  { re: ue, type: "string" },
  { re: st, type: "number" },
  ve(/[A-Za-z_][\w-]*/y, Ns)
], qs = /* @__PURE__ */ new Set([
  "true",
  "false",
  "yes",
  "no",
  "null",
  "on",
  "off",
  "True",
  "False",
  "Yes",
  "No",
  "Null",
  "On",
  "Off",
  "TRUE",
  "FALSE",
  "YES",
  "NO",
  "NULL",
  "ON",
  "OFF"
]), Oo = [
  { re: St, type: "comment" },
  Bt(W),
  Bt(ue),
  { re: /[A-Za-z_][\w .-]*?(?=:(\s|$))/y, type: "property" },
  { re: /[&*!][A-Za-z_][\w:.]*/y, type: "function" },
  { re: st, type: "number" },
  ve(/[A-Za-z_][\w-]*/y, qs)
], Bs = [
  Bt(W),
  { re: st, type: "number" },
  ve(/[A-Za-z_]\w*/y, /* @__PURE__ */ new Set(["true", "false", "null"]))
], Rs = /* @__PURE__ */ new Set([
  "const",
  "let",
  "var",
  "function",
  "return",
  "if",
  "else",
  "for",
  "while",
  "do",
  "switch",
  "case",
  "default",
  "break",
  "continue",
  "class",
  "extends",
  "super",
  "new",
  "this",
  "import",
  "export",
  "from",
  "as",
  "async",
  "await",
  "try",
  "catch",
  "finally",
  "throw",
  "typeof",
  "instanceof",
  "in",
  "of",
  "yield",
  "static",
  "get",
  "set",
  "void",
  "delete",
  "null",
  "undefined",
  "true",
  "false",
  "public",
  "private",
  "protected",
  "readonly",
  "interface",
  "type",
  "enum",
  "implements",
  "namespace",
  "declare",
  "abstract",
  "keyof",
  "satisfies"
]), lt = [
  { re: Hs, type: "comment" },
  { re: Uo, type: "comment" },
  { re: W, type: "string" },
  { re: ue, type: "string" },
  { re: /`(?:\\.|[^`\\])*`/y, type: "string" },
  { re: /-?\b0[xXbBoO][0-9a-fA-F]+\b|-?\b\d+\.?\d*(?:[eE][+-]?\d+)?\b/y, type: "number" },
  ve(/[A-Za-z_$][\w$]*/y, Rs)
], Us = [
  { re: Uo, type: "comment" },
  { re: W, type: "string" },
  { re: ue, type: "string" },
  { re: /@[\w-]+/y, type: "keyword" },
  { re: /!important/y, type: "keyword" },
  { re: /#[0-9a-fA-F]{3,8}\b/y, type: "number" },
  { re: /-?\b\d+\.?\d*[a-zA-Z%]*\b/y, type: "number" },
  { re: /[a-zA-Z-]+(?=\s*:)/y, type: "property" },
  ve(/[a-zA-Z-]+/y, /* @__PURE__ */ new Set())
], ko = [
  { re: /<!--[\s\S]*?-->/y, type: "comment" },
  { re: /<\/?[a-zA-Z][\w:-]*/y, type: "tag" },
  { re: /[a-zA-Z-][\w-]*(?=\s*=)/y, type: "property" },
  { re: W, type: "string" },
  { re: ue, type: "string" },
  { re: /&[\w#]+;/y, type: "keyword" }
], Fs = /* @__PURE__ */ new Set([
  "def",
  "end",
  "if",
  "elsif",
  "else",
  "unless",
  "while",
  "until",
  "for",
  "in",
  "do",
  "class",
  "module",
  "begin",
  "rescue",
  "ensure",
  "raise",
  "return",
  "yield",
  "break",
  "next",
  "redo",
  "retry",
  "case",
  "when",
  "then",
  "and",
  "or",
  "not",
  "nil",
  "true",
  "false",
  "self",
  "super",
  "require",
  "require_relative",
  "include",
  "extend",
  "attr_accessor",
  "attr_reader",
  "attr_writer",
  "private",
  "protected",
  "public",
  "lambda",
  "proc",
  "new"
]), Mo = [
  { re: St, type: "comment" },
  { re: /:[A-Za-z_]\w*[?!]?/y, type: "string" },
  { re: /@{1,2}[A-Za-z_]\w*|\$[A-Za-z_]\w*/y, type: "function" },
  { re: W, type: "string" },
  { re: ue, type: "string" },
  { re: st, type: "number" },
  { re: /[A-Z]\w*/y, type: "property" },
  ve(/[a-z_]\w*[?!]?/y, Fs)
], Zs = /* @__PURE__ */ new Set([
  "class",
  "define",
  "node",
  "inherits",
  "if",
  "elsif",
  "else",
  "unless",
  "case",
  "and",
  "or",
  "in",
  "undef",
  "true",
  "false",
  "default",
  "import",
  "include",
  "require",
  "contain",
  "function",
  "type",
  "application",
  "produces",
  "consumes",
  "private",
  "return",
  "break",
  "next",
  "each",
  "map",
  "filter",
  "reduce",
  "with",
  "String",
  "Integer",
  "Boolean",
  "Array",
  "Hash",
  "Optional",
  "Enum",
  "Variant",
  "Numeric",
  "Float",
  "Undef",
  "Any",
  "Pattern",
  "Regexp",
  "Sensitive",
  "Struct",
  "Tuple",
  "Type",
  "Callable",
  "Data",
  "Scalar"
]), Lo = [
  { re: St, type: "comment" },
  { re: W, type: "string" },
  { re: ue, type: "string" },
  { re: /\$[\w:]+/y, type: "function" },
  { re: /[a-zA-Z_][\w:]*(?=\s*\{)/y, type: "tag" },
  { re: /[a-z_]\w*(?=\s*=>)/y, type: "property" },
  { re: st, type: "number" },
  ve(/[A-Za-z_][\w:]*/y, Zs)
], Ks = {
  bash: nt,
  sh: nt,
  shell: nt,
  zsh: nt,
  yaml: Oo,
  yml: Oo,
  json: Bs,
  javascript: lt,
  js: lt,
  typescript: lt,
  ts: lt,
  css: Us,
  html: ko,
  xml: ko,
  ruby: Mo,
  rb: Mo,
  puppet: Lo,
  pp: Lo
}, Ws = {
  bash: "Bash",
  sh: "Shell",
  shell: "Shell",
  zsh: "Zsh",
  yaml: "YAML",
  yml: "YAML",
  json: "JSON",
  javascript: "JavaScript",
  js: "JavaScript",
  typescript: "TypeScript",
  ts: "TypeScript",
  css: "CSS",
  html: "HTML",
  xml: "XML",
  ruby: "Ruby",
  rb: "Ruby",
  puppet: "Puppet",
  pp: "Puppet",
  plaintext: "Plain Text",
  text: "Plain Text"
};
function Gs(o, e) {
  const r = Ks[e];
  return r ? Is(o, r) : [{ type: "plain", text: o }];
}
var Ys = Object.defineProperty, Js = Object.getOwnPropertyDescriptor, k = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? Js(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && Ys(e, r, t), t;
};
function Xs(o) {
  const e = o.split(`
`);
  for (; e.length && e[0].trim() === ""; ) e.shift();
  for (; e.length && e[e.length - 1].trim() === ""; ) e.pop();
  const r = e.filter((t) => t.trim() !== "").map((t) => {
    var a;
    return ((a = t.match(/^[ \t]*/)) == null ? void 0 : a[0].length) ?? 0;
  }), s = r.length ? Math.min(...r) : 0;
  return e.map((t) => t.slice(s)).join(`
`);
}
function Qs(o) {
  const e = [[]];
  for (const r of o)
    r.text.split(`
`).forEach((s, t) => {
      t > 0 && e.push([]), s && e[e.length - 1].push({ type: r.type, text: s });
    });
  return e;
}
let C = class extends v {
  constructor() {
    super(...arguments), this.language = "", this.filename = "", this.lineNumbers = !1, this.noCopy = !1, this.noHeader = !1, this.noBorder = !1, this.copyLabel = "Copy code", this.copiedLabel = "Copied", this.copiedMessage = "Copied to clipboard", this._code = "", this._copied = !1;
  }
  disconnectedCallback() {
    super.disconnectedCallback(), clearTimeout(this._copyResetTimer);
  }
  _handleSlotChange(o) {
    const r = o.target.assignedNodes({ flatten: !0 }).map((s) => s.textContent ?? "").join("");
    this._code = Xs(r);
  }
  async _copy() {
    var o;
    try {
      await navigator.clipboard.writeText(this._code);
    } catch {
      const e = document.createElement("textarea");
      e.value = this._code, e.style.position = "fixed", e.style.opacity = "0", (o = this.shadowRoot) == null || o.appendChild(e), e.select(), document.execCommand("copy"), e.remove();
    }
    this._copied = !0, clearTimeout(this._copyResetTimer), this._copyResetTimer = setTimeout(() => {
      this._copied = !1;
    }, 1500);
  }
  render() {
    const o = Ws[this.language] ?? this.language, e = Qs(Gs(this._code, this.language));
    return l`
      <div class="block">
        ${this.noHeader ? p : l`
              <div class="header">
                <span class="meta">
                  ${this.filename ? l`<span class="filename">${this.filename}</span>` : p}
                  ${o ? l`<span class="lang">${o}</span>` : p}
                </span>
                ${this.noCopy ? p : l`
                      <button
                        class="copy"
                        type="button"
                        @click=${this._copy}
                        aria-label=${this._copied ? this.copiedLabel : this.copyLabel}
                      >
                        <svg viewBox="0 0 48 48" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                          ${q[this._copied ? "check" : "copy"]}
                        </svg>
                      </button>
                    `}
              </div>
            `}
        <pre tabindex="0"><code>${e.map(
      (r) => l`<span class="line">${r.map(
        (s) => s.type === "plain" ? s.text : l`<span class="tok-${s.type}">${s.text}</span>`
      )}</span>`
    )}</code></pre>
        <span class="visually-hidden" aria-live="polite">${this._copied ? this.copiedMessage : ""}</span>
      </div>
      <slot @slotchange=${this._handleSlotChange}></slot>
    `;
  }
};
C.styles = d`
    :host {
      display: block;
      font-family: var(--vox-font-family-base);
    }

    .block {
      border: 1px solid var(--vox-color-border);
      border-radius: var(--vox-radius-md);
      background-color: var(--vox-color-bg-alt);
      overflow: hidden;
    }

    :host([no-border]) .block {
      border: none;
    }

    .header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: var(--vox-space-3);
      padding: var(--vox-space-2) var(--vox-space-2) var(--vox-space-2) var(--vox-space-4);
      border-bottom: 1px solid var(--vox-color-divider);
      font-size: 12px;
    }

    .meta {
      display: flex;
      align-items: baseline;
      gap: var(--vox-space-3);
      min-width: 0;
      overflow: hidden;
      color: var(--vox-color-text-2);
    }

    .filename {
      color: var(--vox-color-text-1);
      font-family: var(--vox-font-family-mono);
      font-weight: 600;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }

    .lang {
      flex: none;
      text-transform: uppercase;
      letter-spacing: 0.04em;
    }

    .copy {
      flex: none;
      display: inline-flex;
      align-items: center;
      justify-content: center;
      width: 28px;
      height: 28px;
      padding: 0;
      border: none;
      border-radius: var(--vox-radius-sm);
      background: none;
      color: var(--vox-color-text-2);
      cursor: pointer;
    }

    .copy:hover {
      background-color: var(--vox-color-brand-soft);
      color: var(--vox-color-brand-1);
    }

    .copy:focus-visible {
      outline: 2px solid var(--vox-color-brand-1);
      outline-offset: 2px;
    }

    .copy svg {
      width: 15px;
      height: 15px;
    }

    pre {
      margin: 0;
      padding: var(--vox-space-4);
      overflow-x: auto;
      white-space: pre;
      tab-size: 2;
    }

    pre:focus-visible {
      outline: 2px solid var(--vox-color-brand-1);
      outline-offset: -2px;
    }

    code {
      font-family: var(--vox-font-family-mono);
      font-size: 13px;
      line-height: 1.7;
      color: var(--vox-color-text-1);
    }

    .line {
      display: block;
    }

    :host([line-numbers]) code {
      counter-reset: line;
    }

    :host([line-numbers]) .line {
      padding-left: 3.5ch;
      position: relative;
    }

    :host([line-numbers]) .line::before {
      counter-increment: line;
      content: counter(line);
      position: absolute;
      left: 0;
      width: 2.5ch;
      text-align: right;
      color: var(--vox-color-text-3);
      user-select: none;
    }

    .tok-comment {
      color: var(--vox-code-comment);
      font-style: italic;
    }
    .tok-keyword {
      color: var(--vox-code-keyword);
    }
    .tok-string {
      color: var(--vox-code-string);
    }
    .tok-number {
      color: var(--vox-code-number);
    }
    .tok-function {
      color: var(--vox-code-function);
    }
    .tok-property {
      color: var(--vox-code-property);
    }
    .tok-tag {
      color: var(--vox-code-tag);
    }

    slot {
      display: none;
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
k([
  n()
], C.prototype, "language", 2);
k([
  n()
], C.prototype, "filename", 2);
k([
  n({ type: Boolean, attribute: "line-numbers", reflect: !0 })
], C.prototype, "lineNumbers", 2);
k([
  n({ type: Boolean, attribute: "no-copy" })
], C.prototype, "noCopy", 2);
k([
  n({ type: Boolean, attribute: "no-header" })
], C.prototype, "noHeader", 2);
k([
  n({ type: Boolean, attribute: "no-border", reflect: !0 })
], C.prototype, "noBorder", 2);
k([
  n({ attribute: "copy-label" })
], C.prototype, "copyLabel", 2);
k([
  n({ attribute: "copied-label" })
], C.prototype, "copiedLabel", 2);
k([
  n({ attribute: "copied-message" })
], C.prototype, "copiedMessage", 2);
k([
  A()
], C.prototype, "_code", 2);
k([
  A()
], C.prototype, "_copied", 2);
C = k([
  h("vox-code-block")
], C);
var ea = Object.defineProperty, ta = Object.getOwnPropertyDescriptor, Fo = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? ta(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && ea(e, r, t), t;
};
let bt = class extends v {
  constructor() {
    super(...arguments), this.heading = "", this.hasBody = !1, this.hasActions = !1;
  }
  handleBodySlotChange(o) {
    const e = o.target;
    this.hasBody = e.assignedNodes({ flatten: !0 }).length > 0, this.requestUpdate();
  }
  handleActionsSlotChange(o) {
    const e = o.target;
    this.hasActions = e.assignedNodes({ flatten: !0 }).length > 0, this.requestUpdate();
  }
  render() {
    return l`
      <div class="band">
        ${this.heading ? l`<h2 class="heading">${this.heading}</h2>` : p}
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
bt.styles = d`
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
Fo([
  n()
], bt.prototype, "heading", 2);
bt = Fo([
  h("vox-cta-band")
], bt);
var oa = Object.defineProperty, ra = Object.getOwnPropertyDescriptor, Zo = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? ra(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && oa(e, r, t), t;
};
let gt = class extends v {
  constructor() {
    super(...arguments), this.name = "", this.hasIcon = !1;
  }
  handleIconSlotChange(o) {
    const e = o.target;
    this.hasIcon = e.assignedNodes({ flatten: !0 }).length > 0, this.requestUpdate();
  }
  render() {
    return l`
      <span class="icon ${this.hasIcon ? "has-icon" : ""}">
        <slot name="icon" @slotchange=${this.handleIconSlotChange}></slot>
      </span>
      ${this.name ? l`<span class="sr-only">${this.name}: </span>` : p}
      <slot></slot>
    `;
  }
};
gt.styles = d`
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
Zo([
  n()
], gt.prototype, "name", 2);
gt = Zo([
  h("vox-datum")
], gt);
var sa = Object.defineProperty, aa = Object.getOwnPropertyDescriptor, Ko = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? aa(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && sa(e, r, t), t;
};
let mt = class extends v {
  constructor() {
    super(...arguments), this.heading = "";
  }
  render() {
    return l`
      <div class="empty">
        <div class="icon" aria-hidden="true"><slot name="icon"></slot></div>
        <h3 class="heading">${this.heading}</h3>
        <div class="body"><slot></slot></div>
        <div class="actions"><slot name="actions"></slot></div>
      </div>
    `;
  }
};
mt.styles = d`
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
Ko([
  n()
], mt.prototype, "heading", 2);
mt = Ko([
  h("vox-empty-state")
], mt);
var ia = Object.defineProperty, na = Object.getOwnPropertyDescriptor, ao = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? na(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && ia(e, r, t), t;
};
let Rt = class extends v {
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
Rt.styles = d`
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
Rt = ao([
  h("vox-footer")
], Rt);
let yt = class extends v {
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
yt.styles = d`
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
ao([
  n()
], yt.prototype, "heading", 2);
yt = ao([
  h("vox-footer-column")
], yt);
var la = Object.defineProperty, ca = Object.getOwnPropertyDescriptor, zt = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? ca(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && la(e, r, t), t;
};
let Oe = class extends v {
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
Oe.styles = d`
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
zt([
  n({ type: Number })
], Oe.prototype, "cols", 2);
zt([
  n()
], Oe.prototype, "min", 2);
zt([
  n({ reflect: !0 })
], Oe.prototype, "gap", 2);
Oe = zt([
  h("vox-grid")
], Oe);
var da = Object.defineProperty, ha = Object.getOwnPropertyDescriptor, io = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? ha(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && da(e, r, t), t;
};
let We = class extends v {
  constructor() {
    super(...arguments), this.eyebrow = "", this.heading = "", this.hasActions = !1;
  }
  handleActionsSlotChange(o) {
    const e = o.target;
    this.hasActions = e.assignedNodes({ flatten: !0 }).length > 0, this.requestUpdate();
  }
  render() {
    return l`
      <header class="hero">
        ${this.eyebrow ? l`<p class="eyebrow">${this.eyebrow}</p>` : p}
        <h1 class="heading">${this.heading}</h1>
        <div class="body"><slot></slot></div>
        <div class="actions ${this.hasActions ? "has-content" : ""}">
          <slot name="actions" @slotchange=${this.handleActionsSlotChange}></slot>
        </div>
      </header>
    `;
  }
};
We.styles = d`
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
io([
  n()
], We.prototype, "eyebrow", 2);
io([
  n()
], We.prototype, "heading", 2);
We = io([
  h("vox-hero")
], We);
var pa = Object.defineProperty, va = Object.getOwnPropertyDescriptor, jt = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? va(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && pa(e, r, t), t;
};
let Ut = class extends v {
  render() {
    return l`<slot></slot>`;
  }
};
Ut.styles = d`
    :host {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(min(260px, 100%), 1fr));
      gap: var(--vox-space-4);
    }
  `;
Ut = jt([
  h("vox-link-hub")
], Ut);
let Ge = class extends v {
  constructor() {
    super(...arguments), this.href = "#", this.heading = "", this.hasIcon = !1;
  }
  handleIconSlotChange(o) {
    const e = o.target;
    this.hasIcon = e.assignedNodes({ flatten: !0 }).length > 0, this.requestUpdate();
  }
  render() {
    return l`
      <a href=${this.href}>
        <span class="heading">
          <span class="icon ${this.hasIcon ? "has-icon" : ""}" aria-hidden="true">
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
Ge.styles = d`
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
jt([
  n()
], Ge.prototype, "href", 2);
jt([
  n()
], Ge.prototype, "heading", 2);
Ge = jt([
  h("vox-link-hub-item")
], Ge);
var ua = Object.defineProperty, xa = Object.getOwnPropertyDescriptor, no = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? xa(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && ua(e, r, t), t;
};
let Ye = class extends v {
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
Ye.styles = d`
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
no([
  n({ reflect: !0 })
], Ye.prototype, "size", 2);
no([
  n()
], Ye.prototype, "label", 2);
Ye = no([
  h("vox-loader")
], Ye);
var fa = Object.defineProperty, ba = Object.getOwnPropertyDescriptor, Wo = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? ba(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && fa(e, r, t), t;
};
let $t = class extends v {
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
$t.styles = d`
    :host {
      display: block;
      font-family: var(--vox-font-family-base);
    }

    nav {
      display: flex;
      gap: var(--vox-space-1);
      flex-wrap: wrap;
    }

    ::slotted(a),
    ::slotted(span) {
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

    ::slotted([aria-current='page']) {
      background-color: var(--vox-color-brand-3);
      color: var(--vox-color-text-inverse);
      font-weight: 600;
    }
  `;
Wo([
  n()
], $t.prototype, "label", 2);
$t = Wo([
  h("vox-pagination")
], $t);
var ga = Object.defineProperty, ma = Object.getOwnPropertyDescriptor, lo = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? ma(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && ga(e, r, t), t;
};
let Je = class extends v {
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
                ${this.detail ? l`<span class="detail"> — ${this.detail}</span>` : p}
              </footer>
            ` : p}
      </blockquote>
    `;
  }
};
Je.styles = d`
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
lo([
  n()
], Je.prototype, "attribution", 2);
lo([
  n()
], Je.prototype, "detail", 2);
Je = lo([
  h("vox-quote")
], Je);
var ya = Object.defineProperty, $a = Object.getOwnPropertyDescriptor, Pe = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? $a(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && ya(e, r, t), t;
};
let Ft = class extends v {
  render() {
    return l`<slot></slot>`;
  }
};
Ft.styles = d`
    :host {
      display: grid;
      grid-template-columns: repeat(3, max-content) 1fr;
      column-gap: var(--vox-space-4);
    }
  `;
Ft = Pe([
  h("vox-record-list")
], Ft);
let ae = class extends v {
  constructor() {
    super(...arguments), this.heading = "", this.size = "md", this.linkLabel = "View {heading}", this.hasMeta = !1, this.hasEnd = !1;
  }
  handleMetaSlotChange(o) {
    const e = o.target;
    this.hasMeta = e.assignedNodes({ flatten: !0 }).length > 0, this.requestUpdate();
  }
  handleEndSlotChange(o) {
    const e = o.target;
    this.hasEnd = e.assignedNodes({ flatten: !0 }).length > 0, this.requestUpdate();
  }
  render() {
    if (this.size === "sm") {
      const o = l`
        <span class="meta ${this.hasMeta ? "has-meta" : ""}">
          <slot @slotchange=${this.handleMetaSlotChange}></slot>
        </span>
      `;
      return l`
        ${this.href ? l`<a class="cell" href=${this.href}>${this.heading}</a>` : l`<span class="cell">${this.heading}</span>`}
        ${this.hasMeta && this.href ? l`<a class="cell" href=${this.href}>${o}</a>` : l`<span class="cell">${o}</span>`}
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
              <a
                class="arrow"
                href=${this.href}
                aria-label=${this.linkLabel.replace("{heading}", this.heading)}
              >
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                  <path d="M5 12h14" />
                  <path d="m13 6 6 6-6 6" />
                </svg>
              </a>
            ` : p}
      </div>
    `;
  }
};
ae.styles = d`
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
Pe([
  n()
], ae.prototype, "heading", 2);
Pe([
  n()
], ae.prototype, "href", 2);
Pe([
  n({ reflect: !0 })
], ae.prototype, "size", 2);
Pe([
  n({ attribute: "link-label" })
], ae.prototype, "linkLabel", 2);
ae = Pe([
  h("vox-record-list-item")
], ae);
var wa = Object.defineProperty, _a = Object.getOwnPropertyDescriptor, xe = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? _a(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && wa(e, r, t), t;
};
let wt = class extends v {
  constructor() {
    super(...arguments), this.heading = "", this.hasDescription = !1;
  }
  handleDescriptionSlotChange(o) {
    const e = o.target;
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
wt.styles = d`
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
xe([
  n()
], wt.prototype, "heading", 2);
wt = xe([
  h("vox-sponsor-tier")
], wt);
let ie = class extends v {
  constructor() {
    super(...arguments), this.name = "", this.hasBody = !1;
  }
  handleBodySlotChange(o) {
    const e = o.target;
    this.hasBody = e.assignedNodes({ flatten: !0 }).length > 0, this.requestUpdate();
  }
  render() {
    const o = l`
      <div class="logo ${this.logo ? "has-logo" : ""}">
        ${this.logo ? l`<img src=${this.logo} alt=${this.name} />` : p}
      </div>
      <span class="name">${this.name}</span>
      <div class="body ${this.hasBody ? "has-content" : ""}">
        <slot @slotchange=${this.handleBodySlotChange}></slot>
      </div>
    `;
    return this.href !== void 0 ? l`<a
          class="sponsor"
          href=${this.href}
          target=${this.target ?? p}
          >${o}</a
        >` : l`<div class="sponsor">${o}</div>`;
  }
};
ie.styles = d`
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
xe([
  n()
], ie.prototype, "name", 2);
xe([
  n()
], ie.prototype, "href", 2);
xe([
  n()
], ie.prototype, "logo", 2);
xe([
  n()
], ie.prototype, "target", 2);
ie = xe([
  h("vox-sponsor")
], ie);
var Ca = Object.defineProperty, Oa = Object.getOwnPropertyDescriptor, co = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? Oa(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && Ca(e, r, t), t;
};
let Xe = class extends v {
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
Xe.styles = d`
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
co([
  n()
], Xe.prototype, "value", 2);
co([
  n()
], Xe.prototype, "label", 2);
Xe = co([
  h("vox-stat")
], Xe);
var ka = Object.defineProperty, Ma = Object.getOwnPropertyDescriptor, G = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? Ma(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && ka(e, r, t), t;
};
let _t = class extends v {
  constructor() {
    super(...arguments), this.label = "Progress";
  }
  numberSteps() {
    this.querySelectorAll("vox-step").forEach((o, e) => {
      o.number = e + 1;
    });
  }
  render() {
    return l`
      <div class="steps" role="list" aria-label=${this.label}>
        <slot @slotchange=${this.numberSteps}></slot>
      </div>
    `;
  }
};
_t.styles = d`
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
G([
  n()
], _t.prototype, "label", 2);
_t = G([
  h("vox-step-indicator")
], _t);
let F = class extends v {
  constructor() {
    super(...arguments), this.label = "", this.state = "upcoming", this.number = 1, this.completeText = "complete", this.upcomingText = "upcoming";
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
        <span class="label">
          ${this.label}
          ${this.state !== "current" ? l`<span class="visually-hidden">
                (${this.state === "complete" ? this.completeText : this.upcomingText})
              </span>` : p}
        </span>
      </div>
    `;
  }
};
F.styles = d`
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

    .visually-hidden {
      position: absolute;
      width: 1px;
      height: 1px;
      overflow: hidden;
      clip: rect(0 0 0 0);
      white-space: nowrap;
    }
  `;
G([
  n()
], F.prototype, "label", 2);
G([
  n({ reflect: !0 })
], F.prototype, "state", 2);
G([
  n({ type: Number })
], F.prototype, "number", 2);
G([
  n({ attribute: "complete-text" })
], F.prototype, "completeText", 2);
G([
  n({ attribute: "upcoming-text" })
], F.prototype, "upcomingText", 2);
F = G([
  h("vox-step")
], F);
var La = Object.defineProperty, Aa = Object.getOwnPropertyDescriptor, Vt = (o, e, r, s) => {
  for (var t = s > 1 ? void 0 : s ? Aa(e, r) : e, a = o.length - 1, i; a >= 0; a--)
    (i = o[a]) && (t = (s ? i(e, r, t) : i(t)) || t);
  return s && t && La(e, r, t), t;
};
let Zt = class extends v {
  render() {
    return l`<slot></slot>`;
  }
};
Zt.styles = d`
    :host {
      display: block;
    }
  `;
Zt = Vt([
  h("vox-timeline")
], Zt);
let Qe = class extends v {
  constructor() {
    super(...arguments), this.heading = "", this.date = "";
  }
  render() {
    return l`
      <div class="item">
        <span class="dot" aria-hidden="true"></span>
        ${this.date ? l`<div class="date">${this.date}</div>` : p}
        <h3 class="heading">${this.heading}</h3>
        <div class="body"><slot></slot></div>
      </div>
    `;
  }
};
Qe.styles = d`
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
Vt([
  n()
], Qe.prototype, "heading", 2);
Vt([
  n()
], Qe.prototype, "date", 2);
Qe = Vt([
  h("vox-timeline-item")
], Qe);
export {
  xt as VoxAccordion,
  se as VoxAccordionItem,
  U as VoxAlert,
  te as VoxAvatar,
  ft as VoxBadge,
  Ne as VoxBillboard,
  pt as VoxBreadcrumbs,
  j as VoxButton,
  Ze as VoxCalendarTile,
  Ke as VoxCallout,
  Ce as VoxCard,
  ye as VoxCheckbox,
  C as VoxCodeBlock,
  y as VoxCombobox,
  Te as VoxCta,
  bt as VoxCtaBand,
  gt as VoxDatum,
  R as VoxDialog,
  re as VoxDisclosure,
  Fe as VoxDropdown,
  mt as VoxEmptyState,
  P as VoxFileInput,
  Rt as VoxFooter,
  yt as VoxFooterColumn,
  Oe as VoxGrid,
  D as VoxHeader,
  We as VoxHero,
  me as VoxIcon,
  w as VoxInput,
  Nt as VoxInputGroup,
  Ut as VoxLinkHub,
  Ge as VoxLinkHubItem,
  Ye as VoxLoader,
  _e as VoxMenu,
  $t as VoxPagination,
  Je as VoxQuote,
  $e as VoxRadio,
  He as VoxRadioGroup,
  L as VoxRange,
  Ft as VoxRecordList,
  ae as VoxRecordListItem,
  V as VoxSelect,
  E as VoxSeriesNav,
  we as VoxSidenav,
  oe as VoxSidenavGroup,
  qe as VoxSidenavItem,
  ie as VoxSponsor,
  wt as VoxSponsorTier,
  Xe as VoxStat,
  F as VoxStep,
  _t as VoxStepIndicator,
  vt as VoxSubnav,
  Ie as VoxSwitch,
  Be as VoxTab,
  Re as VoxTabPanel,
  qt as VoxTabs,
  B as VoxTextarea,
  ge as VoxThemeToggle,
  Zt as VoxTimeline,
  Qe as VoxTimelineItem,
  ut as VoxToc,
  Ue as VoxTocItem
};
