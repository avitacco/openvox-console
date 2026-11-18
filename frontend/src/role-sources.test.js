import test from 'node:test';
import assert from 'node:assert/strict';
import { splitRoleSources } from './role-sources.js';

test('a direct role is ticked and not listed as coming from a group', () => {
  const { directIds, groupRoles } = splitRoleSources([{ id: 1, name: 'a', via: ['direct'] }]);
  assert.deepEqual([...directIds], [1]);
  assert.deepEqual(groupRoles, []);
});

test('a role held only through groups is not direct and names every group', () => {
  const { directIds, groupRoles } = splitRoleSources([{ id: 2, name: 'b', via: ['ops', 'web'] }]);
  assert.equal(directIds.size, 0);
  assert.deepEqual(groupRoles, [{ name: 'b', groups: ['ops', 'web'] }]);
});

test('a role held both ways is ticked and listed with its group', () => {
  const { directIds, groupRoles } = splitRoleSources([{ id: 3, name: 'c', via: ['direct', 'ops'] }]);
  assert.ok(directIds.has(3));
  assert.deepEqual(groupRoles, [{ name: 'c', groups: ['ops'] }]);
});

test('a role without via is direct', () => {
  const { directIds } = splitRoleSources([{ id: 4, name: 'd' }]);
  assert.ok(directIds.has(4));
});
