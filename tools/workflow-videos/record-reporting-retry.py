from capture import *
login()
with scene('20-reports-grid','06-grid-filter','Compare filtered and company totals','The variance grid is a different working view, organised by project and head for one month. Search for Office Supplies to narrow the rows. The metrics and status counts now describe that filtered selection, while the page separately states the full month company total. Grid export follows these filters, and Month Close always reviews the full month.',expected='Full month company total'):
 goto('/grid');fill('Search','Office Supplies');click('button','Apply');click('link','⤓ Export')
with scene('20-reports-grid','07-empty-reset','Recover from an empty search','A search with no matching head shows an explicit empty state. Clearing filters restores the full month. Use this instead of treating a blank result as zero company spending. Actuals represent payments recorded against each head; an open request can still have a payable balance even when part of its spending already appears in the grid.'):
 fill('Search','No such Lesson E head');click('button','Apply');click('link','Clear filters',nth=0)
with scene('20-reports-grid','08-recoverable-register','Use the separate recoverable view','Recoverable exposure belongs in its own dashboard and register. Our venue deposit still has three thousand rupees outstanding after the expense and adjustment entries. The dashboard groups exposure by expected return date, category, and counterparty. Open the register to distinguish paid out, returned or reconciled, and outstanding amounts instead of reading payout status as recovery status.'):
 goto('/recoverables');click('link','Lesson E Training Venue')
print([(n.get('role'),n.get('name')) for n in snapshot() if n.get('role') in ['link','textbox','combobox','button']][-12:])
