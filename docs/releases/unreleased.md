# Fixes

- **The Calendar tab no longer fails to open.** A reserved-word collision in the smart-list counts query caused a SQL syntax error (`near "all"`) that left the Calendar view with no counts. Opening the Calendar now loads the smart-list counts and the mini-calendar normally.
