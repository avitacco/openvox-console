// Which of a user's roles are theirs directly and which come from groups,
// from the "via" the roles API reports for each: "direct" and/or the name
// of each group granting the role. A role without "via" (an older server)
// is direct.

/**
 * @param {{id: number, name: string, via?: string[]}[]} held
 * @returns {{directIds: Set<number>, groupRoles: {name: string, groups: string[]}[]}}
 */
export function splitRoleSources(held) {
  const directIds = new Set();
  const groupRoles = [];
  for (const role of held) {
    const via = role.via && role.via.length > 0 ? role.via : ['direct'];
    if (via.includes('direct')) directIds.add(role.id);
    const groups = via.filter((v) => v !== 'direct');
    if (groups.length > 0) groupRoles.push({ name: role.name, groups });
  }
  return { directIds, groupRoles };
}
