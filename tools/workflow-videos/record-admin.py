from capture import *
def brief():
 print([(x.get('role'),x.get('name'),x.get('ref')) for x in snapshot() if x.get('role') in ['textbox','button','combobox','checkbox','spinbutton']],flush=True)
video('25-month-close','Close a month and reopen it with a reason','Administrators and budget owners',['Create a blank month','Review full-month close indicators','Lock changes','Verify read-only budgets','Unlock with an audit reason'],['Closing is a control on budget and payment changes, not proof that bank balances reconcile.','An authorised person can unlock a month; the reason belongs in the audit trail.'])
goto('/months')
with scene('25-month-close','01-create','Start a separate monthly plan','Monthly Plans separates one period from another. You can copy a previous plan or start blank. Here we create November twenty twenty-six as a blank demonstration month. A blank plan does not copy last month’s budgets or payments. After creation, review the period in the grid before entering amounts or closing it.', '2026-11'):
 fill('New month','2026-11');select('Plan type','blank');click('button','Create Month')
goto('/grid?month=2026-11')
brief()
