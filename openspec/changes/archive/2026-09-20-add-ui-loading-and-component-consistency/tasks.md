## 1. Loading feedback

- [x] 1.1 Add a shared helper that shows a loading indicator only once work has been running long enough to be worth acknowledging, and that cannot leave an indicator behind on either the success or the failure path; verify the timer is cleared unconditionally
- [x] 1.2 Apply it to every data view's primary fetch, targeting the container that view renders into; verify each module still parses and imports the helper
- [x] 1.3 Verify under real latency that the indicator appears during a slow fetch and is gone once content renders
- [x] 1.4 Verify no view is left holding an indicator after load, across the console's pages

## 2. Component property corrections

- [x] 2.1 Audit every component property the console sets against the values each component declares, resolving property inheritance; verify the audit reports no unknown attributes
- [x] 2.2 Correct alert variants passed values belonging to other components; verify in a browser that the corrected variants render with colour and icon and the old ones did not
- [x] 2.3 Correct the button variant passed a value the component does not accept

## 3. Controls suited to their data

- [x] 3.1 Convert filters whose options come from recorded data to type-ahead, leaving short fixed enumerations as direct selection; verify typing narrows the options and selecting one drives the query
- [x] 3.2 Join the deploy trigger and its source picker into one attached control; verify the two align as a single control rather than stretching to different heights

## 4. Verification

- [x] 4.1 Confirm every frontend module parses and the bundle builds
- [x] 4.2 Load every console page in a browser and confirm no scripting errors
