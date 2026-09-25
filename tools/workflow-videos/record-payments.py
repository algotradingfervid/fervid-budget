from capture import *
V='10-accounts-ownership'
video(V,'Accounts queue, holds and ownership','Accounts team',['Review approved requests','Explain and release holds','Claim and release payment work'],['A hold does not cancel approval or move money.','Ownership avoids conflicting payment entry; a manager handles reassignment.'])
with scene(V,'01-queue','Find an approved request','The Accounts queue is the operational work list for approved requests. Open a request to compare its approved amount, payment status, requester, and supporting history. Here Anil is working on an approved twelve thousand rupee reimbursement. Approval authorizes the request; Accounts still needs to record what was actually paid.', 'Lesson P Full reimbursement'):
 goto('/accounts-queue');click('link','PR-2026-000001')
with scene(V,'02-hold','Explain why payment must wait','Use Put on hold when an approved request cannot proceed yet. A specific reason makes the delay understandable to the requester and other Accounts staff. In this example, the team is waiting for a verified bank reference. This action pauses processing; it does not reject the request or undo the approval.'):
 click('button','Put on hold')
 print([(x.get('role'),x.get('name')) for x in snapshot() if x.get('role') in ['button','textbox','combobox']])
