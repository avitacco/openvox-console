import { fetchJSON, sendJSON, escapeHtml, requirePermission, applyAvatar, withLoading, t, formatDateTime } from './app.js';
import { createPasswordRules } from './password-rules.js';
import { splitRoleSources } from './role-sources.js';

if (requirePermission('rbac:admin')) {
  const results = document.getElementById('results');
  const createError = document.getElementById('create-error');
  const newUsername = document.getElementById('new-username');
  const newPassword = document.getElementById('new-password');
  const newFirstName = document.getElementById('new-first-name');
  const newLastName = document.getElementById('new-last-name');
  const newEmail = document.getElementById('new-email');

  let allRoles = [];
  const newPasswordRules = createPasswordRules(document.getElementById('new-password-rules'));

  // A locked account: when the lock ends, and the way to end it now.
  function lockHTML(user) {
    if (!user.lockedUntil) return '';
    const until = t('Locked until {when}', { when: formatDateTime(user.lockedUntil) });
    return `
      <div class="vox-display-flex vox-items-center vox-gap-sm vox-flex-wrap vox-m-top-sm">
        <vox-badge variant="warning">${escapeHtml(until)}</vox-badge>
        <vox-button data-unlock-id="${user.id}" variant="alt" size="sm">${escapeHtml(t('Unlock'))}</vox-button>
      </div>`;
  }

  // Sets a user's password, as an administrator. It is also how a
  // locked-out user is let back in with a password they know.
  function openSetPasswordDialog(user) {
    const dialog = document.createElement('vox-dialog');
    dialog.heading = t('Set a password for {user}', { user: user.username });
    dialog.setAttribute('light-dismiss', '');
    dialog.innerHTML = `
      <vox-input type="password" autocomplete="new-password" data-password label="${escapeHtml(t('New password'))}"></vox-input>
      <div data-rules></div>
      <p class="vox-ts-sm vox-color-text-2">${escapeHtml(t('If the account is locked, this unlocks it. Sessions the user already has open carry on.'))}</p>
      <div data-error></div>
      <div slot="footer">
        <vox-button variant="alt" data-action="cancel">${escapeHtml(t('Cancel'))}</vox-button>
        <vox-button variant="brand" data-action="save">${escapeHtml(t('Set password'))}</vox-button>
      </div>`;
    const errorEl = dialog.querySelector('[data-error]');
    const passwordEl = dialog.querySelector('[data-password]');
    const rules = createPasswordRules(dialog.querySelector('[data-rules]'));
    dialog.addEventListener('vox-close', () => dialog.remove());
    dialog.querySelector('[data-action="cancel"]').addEventListener('click', () => dialog.close());
    dialog.querySelector('[data-action="save"]').addEventListener('click', async () => {
      errorEl.innerHTML = '';
      rules.clear();
      if (!passwordEl.value) {
        errorEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(t('Type the new password.'))}</vox-alert>`;
        return;
      }
      try {
        await sendJSON(`/api/v1/users/${user.id}`, 'PUT', { password: passwordEl.value });
        dialog.close();
        await load();
      } catch (err) {
        if (err.rules) {
          rules.showFailed(err.rules);
          errorEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(t('That password does not meet the rules above.'))}</vox-alert>`;
        } else {
          errorEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
        }
      }
    });
    document.body.appendChild(dialog);
    dialog.show();
  }

  function showCreateError(message) {
    createError.innerHTML = `<vox-alert variant="danger">${escapeHtml(message)}</vox-alert>`;
  }

  function roleCheckboxes(user) {
    return allRoles
      .map((role) => {
        const checked = user.roleIds.has(role.id) ? 'checked' : '';
        return `
          <vox-checkbox data-user-id="${user.id}" data-role-id="${role.id}" ${checked}>
            ${escapeHtml(role.name)}
          </vox-checkbox>`;
      })
      .join('');
  }

  // What a user holds through groups, which cannot be unticked here: the
  // roles that come from a group (naming it), and the groups themselves.
  // A role held both ways stays ticked above and is listed here as well.
  function groupHTML(user) {
    if (user.groups.length === 0) return '';
    const viaRoles = user.groupRoles
      .map((r) => `<vox-badge variant="tip">${escapeHtml(t('{role} via {groups}', { role: r.name, groups: r.groups.join(', ') }))}</vox-badge>`)
      .join(' ');
    const groups = user.groups
      .map((g) => `<vox-badge variant="neutral">${escapeHtml(g.name)}</vox-badge>`)
      .join(' ');
    return `
      <div class="vox-display-flex vox-flex-wrap vox-items-center vox-gap-sm vox-m-top-sm">
        <span class="vox-ts-sm vox-color-text-2">${escapeHtml(t('In groups:'))}</span> ${groups}
      </div>
      ${viaRoles ? `<div class="vox-display-flex vox-flex-wrap vox-gap-sm vox-m-top-sm">${viaRoles}</div>` : ''}`;
  }

  function renderUsers(users) {
    if (users.length === 0) {
      results.innerHTML = `
        <vox-empty-state heading="${t('No users yet')}">
          <vox-icon slot="icon" name="person" size="lg"></vox-icon>
          Create the first user above.
        </vox-empty-state>`;
      return;
    }

    const fullName = (u) => [u.firstName, u.lastName].filter(Boolean).join(' ');

    const rows = users
      .map(
        (u) => `
      <tr>
        <td><vox-avatar data-avatar-id="${u.id}" size="sm"></vox-avatar></td>
        <td>${escapeHtml(u.username)}${lockHTML(u)}</td>
        <td>${escapeHtml(fullName(u))}</td>
        <td>${escapeHtml(u.email || '')}</td>
        <td><div class="vox-display-flex vox-flex-wrap vox-gap-sm">${roleCheckboxes(u)}</div>${groupHTML(u)}</td>
        <td>
          <div class="vox-display-flex vox-gap-sm">
            <vox-button data-password-id="${u.id}" variant="alt" size="sm">${t('Set password')}</vox-button>
            <vox-button data-delete-id="${u.id}" variant="danger" size="sm">${t('Delete')}</vox-button>
          </div>
        </td>
      </tr>`
      )
      .join('');

    results.innerHTML = `
      <div class="vox-table-wrap">
        <table class="vox-table vox-table--striped">
          <thead>
            <tr><th scope="col"></th><th scope="col">${t('Username')}</th><th scope="col">${t('Name')}</th><th scope="col">${t('Email')}</th><th scope="col">${t('Roles')}</th><th scope="col"></th></tr>
          </thead>
          <tbody>${rows}</tbody>
        </table>
      </div>`;

    results.querySelectorAll('vox-avatar[data-avatar-id]').forEach((el) => {
      const u = users.find((user) => String(user.id) === el.dataset.avatarId);
      if (u) applyAvatar(el, { firstName: u.firstName, lastName: u.lastName, email: u.email, username: u.username });
    });

    results.querySelectorAll('vox-checkbox[data-role-id]').forEach((el) => {
      el.addEventListener('change', async () => {
        const userId = el.dataset.userId;
        const roleId = el.dataset.roleId;
        try {
          if (el.checked) {
            await sendJSON(`/api/v1/users/${userId}/roles/${roleId}`, 'POST');
          } else {
            await sendJSON(`/api/v1/users/${userId}/roles/${roleId}`, 'DELETE');
          }
        } catch (err) {
          el.checked = !el.checked;
          showCreateError(err.message);
        }
      });
    });

    results.querySelectorAll('vox-button[data-unlock-id]').forEach((el) => {
      el.addEventListener('click', async () => {
        el.disabled = true;
        try {
          await sendJSON(`/api/v1/users/${el.dataset.unlockId}/unlock`, 'POST');
          await load();
        } catch (err) {
          el.disabled = false;
          showCreateError(err.message);
        }
      });
    });

    results.querySelectorAll('vox-button[data-password-id]').forEach((el) => {
      el.addEventListener('click', () => {
        const user = users.find((u) => String(u.id) === el.dataset.passwordId);
        if (user) openSetPasswordDialog(user);
      });
    });

    results.querySelectorAll('vox-button[data-delete-id]').forEach((el) => {
      el.addEventListener('click', async () => {
        try {
          await sendJSON(`/api/v1/users/${el.dataset.deleteId}`, 'DELETE');
          await load();
        } catch (err) {
          showCreateError(err.message);
        }
      });
    });
  }

  async function load() {
    try {
      const [users, roles] = await withLoading(results, () =>
        Promise.all([fetchJSON('/api/v1/users'), fetchJSON('/api/v1/roles')]));
      allRoles = roles;
      const withRoles = await Promise.all(
        users.map(async (u) => {
          const [held, groups] = await Promise.all([
            fetchJSON(`/api/v1/users/${u.id}/roles`),
            fetchJSON(`/api/v1/users/${u.id}/groups`),
          ]);
          // Only direct assignments are the ones the checkboxes change.
          const { directIds, groupRoles } = splitRoleSources(held);
          return { ...u, roleIds: directIds, groups, groupRoles };
        })
      );
      renderUsers(withRoles);
    } catch (err) {
      results.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
    }
  }

  document.getElementById('create-user').addEventListener('click', async () => {
    createError.innerHTML = '';
    newPasswordRules.clear();
    const username = newUsername.value.trim();
    const password = newPassword.value;
    if (!username || !password) {
      showCreateError(t('Username and password are required.'));
      return;
    }
    try {
      await sendJSON('/api/v1/users', 'POST', {
        username,
        password,
        firstName: newFirstName.value.trim(),
        lastName: newLastName.value.trim(),
        email: newEmail.value.trim(),
      });
      newUsername.value = '';
      newPassword.value = '';
      newFirstName.value = '';
      newLastName.value = '';
      newEmail.value = '';
      await load();
    } catch (err) {
      // A password the account policy refuses: the rules it fails are
      // marked beside the field, and everything typed is kept.
      if (err.rules) {
        newPasswordRules.showFailed(err.rules);
        showCreateError(t('That password does not meet the rules shown under it.'));
        return;
      }
      showCreateError(err.message);
    }
  });

  load();
}
