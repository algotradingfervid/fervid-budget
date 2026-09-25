from capture import *
with scene('20-reports-grid','03-head','Inspect the payments behind actuals','Office Supplies has recorded actual spending in this demonstration period. Opening its detail reveals the active payments behind that amount. Recoverable deposits and voided payments are excluded. The list is limited to the latest three hundred payments per month and the viewer’s payment permissions, so a restricted user may see fewer records than the report total.'):
 click('link','View budget and payments',nth=2)
print([(n.get('name'),n.get('url')) for n in snapshot() if n.get('role')=='link'][-12:])
