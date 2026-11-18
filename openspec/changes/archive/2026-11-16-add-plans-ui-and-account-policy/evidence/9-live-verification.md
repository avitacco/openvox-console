# Live verification

Run on the development stack on 2026-11-16: two clustered console
instances in the `all` mode on one database (ports 8080 and 8091), the
real openvoxserver and openvoxdb, and one real node
(`task-e2e-agent.example.com`) running the node agent client. Pages were
driven in a headless browser. Screenshots are in `screenshots/`.

Test users, roles, the node group, the saved plans, the schedule and the
policy change were removed afterwards; the jobs remain in the job
history.

## Plans (task 9.2)

| Check | Result |
| --- | --- |
| Build a plan in the editor: add a task step, add a run step, move the run step above the task step, save | Saved with the run step first. What was typed in the task step moved with it. `plan-view.png` |
| A save the editor can refuse itself (a required task parameter left empty) | Shown on the step; nothing lost. `plan-editor-refused.png` |
| A save the console refuses for a step (unknown environment on step 2) | "Step 2 (run): environment … not found on the Puppet server" shown on step 2 only; name, description and both steps kept |
| A save under a name already in use | Refused beside the name field; everything kept |
| Run "Echo, wait, echo" (three task steps) on a node group from the Run plan dialog | Job 3925. The job page went from "Succeeded, Running, Waiting" to "Succeeded, Succeeded, Running" to all succeeded without a reload. `plan-job-succeeded.png` |
| Run "Fails at step two" | Job 3929: step 1 succeeded, step 2 failed with the task's own error, step 3 "Not run", plan failed. `plan-job-failed.png` |
| A plan whose first step is a Puppet run that fails | Job 3922: step 1 failed, step 2 "Not run" (the dev stack's Puppet runs fail on a class missing from the deployed code; unrelated to this change) |
| A plan with a sensitive value: the dialog asks for it, labelled with its step, and does not start without it | "Problem: required" on the field. With the value: job 3933 succeeded, the node received it (the fixture reported its length, 20), and it reads as redacted on the job. `run-plan-dialog.png` |
| A scoped operator (role on one node group) | Lists all four saved plans. Preview of `nodes[certname] { }` shows their one node; a plan run by that query (job 3936) ran on that node only; a subquery is refused; a named node outside their group is refused whole |
| A read-only user (`orchestrator:read`) | Sees the plans and a plan's steps; no New plan, Run, Edit or Delete; the API refuses create and run with 403. `plans-reader.png` |
| A step's own job page | Says "Step 2 of 3 in the plan" with a link back to the plan |
| The job list | One line for each plan, with its nodes and where they came from; no lines for steps |
| "Schedule instead" from the Run plan dialog | Opens the schedule form with the plan's name, both steps and the chosen node group filled in. Saving without the sensitive value is refused on the field; with it, schedule 481 is saved. `schedule-plan.png` |
| The saved plan is then changed to three different steps | The schedule still reads its two steps |
| The schedule comes due with two instances running | One firing, one plan job (3938), triggered by the owner and naming the schedule. It ran the schedule's two steps, not the saved plan's three. The node received the sensitive value (length 22); it is redacted on the schedule and the job |
| Editing the schedule | The Plan select offers "Keep the steps shown below" and the saved plans; the sensitive field says a value is stored and need not be re-entered |

## Account policy (task 9.3)

| Check | Result |
| --- | --- |
| The Account policy page on a database that has never changed it | Shows the defaults: 12, no required kinds, 10 failures, 15 minutes |
| A minimum length of 4 | Refused beside the field: "The minimum password length must be between 8 and 72" |
| Change to 14 characters, a digit, a symbol, lock after 4 failures for 2 minutes, through instance 1 | Saved. `account-policy.png` |
| The policy as instance 2 reads it, with no restart | The new values. Creating a user there with a 13-letter password is refused with `min_length`, `digit`, `symbol` |
| The create-user form with a password failing three rules | The three rules are marked "Not met" under the field; the username and email typed are kept |
| The change-password form in Preferences | Lists the four rules; a password with no digit or symbol marks those two; what was typed is kept. `preferences-rules.png` |
| Four wrong passwords for one account, alternating between the two instances | The account is locked: the correct password is refused on both instances |
| The response for the locked account with the right password, for another account with a wrong password, and for an unknown username | Byte for byte the same: 401 `{"error":"invalid credentials"}` |
| Four more wrong passwords while locked | The lock's end time did not move |
| The Users page | The account shows "Locked until" with the time, and an Unlock button |
| Unlock from the Users page | The badge goes; the user logs in at once on the other instance |
| Lock again and wait | Refused one minute in; accepted after the two minutes had passed |
| The login page after a failed login | "Invalid username or password. After several failed attempts in a row, an account is locked for a while: wait, or ask an administrator to unlock it." `login-failed.png` |
| Start on an empty database with a 6-character first-start password | No user created; login refused; the log says "CONSOLE_BOOTSTRAP_ADMIN_PASSWORD does not meet the account policy … it needs at least 12 characters"; the password is not in the log |
| Start again on the same database with a 21-character password | "bootstrapped first admin user"; login succeeds |

The Users page screenshot was not kept: it shows the real accounts on
the development stack.
