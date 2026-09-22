// Evaluates gettext Plural-Forms rules.
//
// A catalogue's header carries its language's rule, for example:
//
//   English, German:  nplurals=2; plural=(n != 1);
//   French:           nplurals=2; plural=(n > 1);
//   Chinese, Korean:  nplurals=1; plural=0;
//   Russian:          nplurals=3; plural=(n%10==1 && n%100!=11 ? 0
//                       : n%10>=2 && n%10<=4 && (n%100<10 || n%100>=20) ? 1 : 2);
//
// The expression is a C conditional over `n`, and this file evaluates it
// with a small recursive-descent parser.
//
// Deliberately not `new Function(expr)`: the rule arrives inside a
// catalogue the browser fetches at runtime, and turning fetched data
// into executable code is a hole with no upside here. The grammar is
// tiny, so parsing it costs about a hundred lines and can only ever
// produce a number.
//
// No DOM access anywhere in this file, so it can be unit-tested under
// plain node - see plural.test.js. Getting a plural rule silently wrong
// is the failure this whole module exists to prevent, so it is tested
// rather than assumed.

/** The rule used when a catalogue declares none. */
export const DEFAULT_RULE = 'nplurals=2; plural=(n != 1);';

/**
 * Parses a Plural-Forms header.
 *
 * @param {string} header e.g. "nplurals=3; plural=(n != 1);"
 * @returns {{nplurals: number, select: (n: number) => number}}
 *   `select` returns the form index for a count, always clamped into
 *   range so a malformed rule cannot index past the translations.
 */
export function parsePluralForms(header) {
  const source = typeof header === 'string' && header.trim() ? header : DEFAULT_RULE;

  const npluralsMatch = /nplurals\s*=\s*(\d+)/.exec(source);
  const nplurals = npluralsMatch ? parseInt(npluralsMatch[1], 10) : 2;

  const pluralMatch = /plural\s*=\s*([^;]+)/.exec(source);
  const expression = pluralMatch ? pluralMatch[1].trim() : '(n != 1)';

  let ast;
  try {
    ast = parse(expression);
  } catch (e) {
    // A rule we cannot parse must not take the page down. English-style
    // two-form behaviour is a safe, obvious fallback.
    if (typeof console !== 'undefined') {
      console.warn(`Unparseable Plural-Forms rule "${source}"; using the default.`, e);
    }
    return { nplurals: 2, select: (n) => (n === 1 ? 0 : 1) };
  }

  const select = (n) => {
    const count = Number(n);
    if (!Number.isFinite(count)) return 0;
    let index = evaluate(ast, Math.abs(Math.trunc(count)));
    // A boolean result is how the two-form rules are written.
    if (index === true) index = 1;
    if (index === false) index = 0;
    index = Math.trunc(Number(index));
    if (!Number.isFinite(index) || index < 0) return 0;
    return index >= nplurals ? nplurals - 1 : index;
  };

  return { nplurals, select };
}

// --- tokenizer -------------------------------------------------------

const PUNCTUATION = ['<=', '>=', '==', '!=', '&&', '||', '?', ':', '(', ')', '<', '>', '+', '-', '*', '/', '%', '!'];

function tokenize(input) {
  const tokens = [];
  let i = 0;

  while (i < input.length) {
    const ch = input[i];

    if (/\s/.test(ch)) {
      i += 1;
      continue;
    }

    if (/\d/.test(ch)) {
      let j = i;
      while (j < input.length && /\d/.test(input[j])) j += 1;
      tokens.push({ type: 'number', value: parseInt(input.slice(i, j), 10) });
      i = j;
      continue;
    }

    if (ch === 'n') {
      tokens.push({ type: 'n' });
      i += 1;
      continue;
    }

    const punct = PUNCTUATION.find((p) => input.startsWith(p, i));
    if (punct) {
      tokens.push({ type: punct });
      i += punct.length;
      continue;
    }

    throw new Error(`unexpected character ${JSON.stringify(ch)} at ${i}`);
  }

  tokens.push({ type: 'end' });
  return tokens;
}

// --- parser ----------------------------------------------------------
//
// Precedence, lowest first, matching C:
//   ?:  ||  &&  ==/!=  </>/<=/>=  +/-  *,/,%  unary!  primary

function parse(input) {
  const tokens = tokenize(input);
  let pos = 0;

  const peek = () => tokens[pos].type;
  const next = () => tokens[pos++];
  const expect = (type) => {
    if (peek() !== type) throw new Error(`expected ${type}, found ${peek()}`);
    return next();
  };

  function conditional() {
    const test = logicalOr();
    if (peek() !== '?') return test;
    next();
    const then = conditional();
    expect(':');
    const otherwise = conditional();
    return { op: '?:', test, then, otherwise };
  }

  function binary(sub, ops) {
    let left = sub();
    while (ops.includes(peek())) {
      const op = next().type;
      left = { op, left, right: sub() };
    }
    return left;
  }

  const logicalOr = () => binary(logicalAnd, ['||']);
  const logicalAnd = () => binary(equality, ['&&']);
  const equality = () => binary(relational, ['==', '!=']);
  const relational = () => binary(additive, ['<', '>', '<=', '>=']);
  const additive = () => binary(multiplicative, ['+', '-']);
  const multiplicative = () => binary(unary, ['*', '/', '%']);

  function unary() {
    if (peek() === '!') {
      next();
      return { op: '!', operand: unary() };
    }
    return primary();
  }

  function primary() {
    if (peek() === 'number') return { op: 'literal', value: next().value };
    if (peek() === 'n') {
      next();
      return { op: 'n' };
    }
    if (peek() === '(') {
      next();
      const inner = conditional();
      expect(')');
      return inner;
    }
    throw new Error(`unexpected ${peek()}`);
  }

  const ast = conditional();
  if (peek() !== 'end') throw new Error(`trailing input at token ${pos}`);
  return ast;
}

// --- evaluator -------------------------------------------------------

function evaluate(node, n) {
  switch (node.op) {
    case 'literal':
      return node.value;
    case 'n':
      return n;
    case '!':
      return !truthy(evaluate(node.operand, n));
    case '?:':
      return truthy(evaluate(node.test, n))
        ? evaluate(node.then, n)
        : evaluate(node.otherwise, n);
    default:
      break;
  }

  const left = evaluate(node.left, n);
  const right = evaluate(node.right, n);

  switch (node.op) {
    case '||':
      return truthy(left) || truthy(right);
    case '&&':
      return truthy(left) && truthy(right);
    case '==':
      return num(left) === num(right);
    case '!=':
      return num(left) !== num(right);
    case '<':
      return num(left) < num(right);
    case '>':
      return num(left) > num(right);
    case '<=':
      return num(left) <= num(right);
    case '>=':
      return num(left) >= num(right);
    case '+':
      return num(left) + num(right);
    case '-':
      return num(left) - num(right);
    case '*':
      return num(left) * num(right);
    case '/':
      // Integer division, as in C - the rules assume it.
      return Math.trunc(num(left) / num(right));
    case '%':
      return num(left) % num(right);
    default:
      throw new Error(`unknown operator ${node.op}`);
  }
}

function truthy(v) {
  return v === true || (typeof v === 'number' && v !== 0);
}

function num(v) {
  if (v === true) return 1;
  if (v === false) return 0;
  return v;
}
