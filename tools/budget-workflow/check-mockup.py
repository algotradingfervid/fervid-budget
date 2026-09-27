from browser import *
def named_contains(role,needle):
 matches=[n for n in snap() if n.get('role')==role and needle in n.get('name','')]
 assert len(matches)==1,(role,needle,matches)
 return matches[0]['ref']
def toggle(name):call('click',named_contains('generic',name))
def amount(name,value):call('fill',ref('spinbutton',name),value)
def result(expr):return call('eval',expr)['result']
call('goto','http://127.0.0.1:8931/?v=3');click('button','Continue to amounts →')
assert result('document.querySelectorAll("#amountGroups details").length')=='7'
assert result('document.querySelectorAll("#amountGroups details[open]").length')=='0'
toggle('Operations 3 expense heads');toggle('Office Rent 2 budget lines');click('button','Add line to Office Rent');fill('Line 3 description for Office Rent','Service charges');amount('Line 3 amount for Office Rent','2000');require('₹1,92,000.00');click('button','Remove line 3 from Office Rent');require('₹1,90,000.00');click('button','Collapse all');shot('mockup-v3-collapsed')
call('goto','http://127.0.0.1:8931/?v=3');call('click',named_contains('radio','Start with a blank budget'));click('button','Continue to amounts →');require('No projects, heads, or lines yet.');assert result('document.querySelectorAll("#amountGroups details").length')=='0';assert result('document.querySelectorAll("[data-amount]").length')=='0';require('₹0.00');shot('mockup-v3-blank')
click('button','＋ Add project');fill('Project name','Training');click('button','Add project');assert result('document.querySelectorAll("#amountGroups details[open]").length')=='0';toggle('Training');click('button','Add head to Training');fill('Expense head name','Venue');click('button','Add head');require('Venue');assert result('document.querySelectorAll(".head-block[open]").length')=='0';toggle('Venue');click('button','Add line to Venue');fill('Line 1 description for Venue','Room hire');amount('Line 1 amount for Venue','10000');click('button','Add line to Venue');fill('Line 2 description for Venue','Equipment hire');amount('Line 2 amount for Venue','2500');require('₹12,500.00');shot('mockup-v3-lines');click('button','Collapse all');click('button','Review budget →');require('₹12,500.00');require('Project: Training');require('Head: Training / Venue');shot('mockup-v3-review');click('button','← Edit amounts');require('₹12,500.00');call('resize','390','844');click('button','Expand all');assert result('document.documentElement.scrollWidth <= innerWidth')=='true';shot('mockup-v3-mobile');call('resize','1440','1000');print('PASS: collapsed defaults; copied line add/remove totals; genuinely empty blank; inline project/head creation; two-line rollup; review; preserved edits; mobile layout.')
