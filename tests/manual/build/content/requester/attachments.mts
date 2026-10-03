import { page, prose, section, subsection, shot, steps, note, warning, tip, table, faq } from '../../schema.mts';

export default page({
  section: 'requester',
  slug: 'attachments',
  title: 'Attaching documents',
  summary: 'Adding invoices and receipts, and what to do when you do not have one yet.',
  blocks: [
    prose(
      `A request with the invoice attached is a request your approver can decide without writing back
       to you. Every screen you can raise or correct a request on carries an uploader, and everything
       you attach stays with the request for the rest of its life. Only the person who raised a
       request can add documents to it.`,
    ),

    section('Attaching a document as you raise a request'),
    shot('requester/form-vendor-invoice', 'The uploader sits in the Purpose and documents fieldset, below the purpose box.'),
    prose(
      `The uploader is labelled **Add invoice, receipt or proof**, with the accepted shape underneath:
       *PDF, JPG or PNG up to 10 MiB*. The size is a setting your administrator controls — any whole
       number from 1 to 20 MiB — so the number you see may differ from the one here. Below that is a
       **Choose File** control, reading
       *No file chosen* until you pick something.`,
    ),
    steps([
      { text: 'Select **Choose File**.' },
      { text: 'Pick the invoice, receipt or proof from your machine.' },
      {
        text: 'Fill in the rest of the form and select **Submit request**.',
        note: 'The document and the request are written together. If the request is refused, the file is not kept either.',
      },
    ]),
    note(
      `The form takes one file per submission. To attach a second, submit the request and then open it
       for editing — the edit form’s uploader says **Add another document** and keeps everything
       already there.`,
    ),

    section('When the label carries an asterisk'),
    prose(
      `Your administrator can make a supporting document compulsory. When they have, **Supporting
       document** is marked with a red asterisk instead of *optional*, and a further field appears
       below the uploader: **If you cannot attach a document, say why**. Its hint says exactly what the
       rule is — *A missing document never blocks you — it asks for this instead, and your approver
       sees it* — and the placeholder shows the kind of answer wanted: *Vendor posts the invoice; it
       arrives Monday.*`,
    ),
    tip(
      `Submit with neither a file nor a reason and the form returns with
       \`attach a supporting document, or say why you cannot\`. One or the other is always enough.`,
    ),
    prose(
      `Whatever you write there is not buried. It appears on the request as a banner headed **No
       document was attached**, carrying your sentence, where the approver and Accounts both read it.`,
    ),

    section('Reading the documents on a request'),
    shot('requester/detail-pending', 'The Attachments section on a request with nothing attached: the count says 0 files and the body says so too.'),
    prose(
      `Every request has an **Attachments** section between the details and the conversation. The count
       sits at the right of the heading — *0 files*, *1 file*, and so on. With nothing attached the
       body reads **No documents attached.**`,
    ),
    prose(
      `Each document that is there gets a row: a small square with the file type, the file name, its
       size, the date it was added, and a **Download** button. Uploading also drops an **Attachment
       added** line, with the file name and size, into **History and conversation**, so the story of
       the request shows when each document arrived.`,
    ),

    subsection('Downloading'),
    prose(
      `**Download** opens the file exactly as it was uploaded. You can download documents on any
       request you can see, which for a requester means your own requests.`,
    ),

    section('Adding a document later'),
    table(
      ['Request status', 'Can you add a document?', 'Where'],
      [
        ['`pending` — awaiting approval', 'Yes', '**Edit request** → **Add another document**'],
        ['`returned` — sent back to you', 'Yes', 'The correction form → **Attach the corrected document**'],
        ['`approved` and on hold with Accounts', 'Yes', 'The hold banner → **Add a document** → **Attach it**'],
        ['`approved` and everything after it, otherwise', 'No', 'The request is locked; ask in the conversation instead'],
        ['`rejected`, `withdrawn`, `cancelled`', 'No', 'The request is closed'],
      ],
    ),
    prose(
      `On a returned request the uploader is worded for the job in hand — **Attach the corrected
       document** — and sits with the fields your approver is most likely to have asked about. See
       [When a request comes back to you](page:requester/when-a-request-is-returned).`,
    ),
    prose(
      `When Accounts put an approved request on hold — often to ask for a missing receipt — the amber
       hold banner on the request carries its own **Add a document** control and an **Attach it**
       button. It takes one file at a time, and it is there only while the hold lasts. See
       [Getting paid](page:requester/getting-paid).`,
    ),

    section('What you cannot do'),
    warning(
      `A document cannot be removed once it is attached. There is no delete control, and no route
       behind one. If you attach the wrong file, attach the right one as well and say which is which
       in the conversation — the record of what was uploaded is deliberately permanent.`,
      'Uploads are permanent',
    ),
    prose(
      `The size figure the uploader quotes is the real limit. A larger file is refused with a message
       quoting that same figure — for example \`files must be 10 MiB or smaller. Choose the attachment
       again after correcting it\`. Your administrator can raise the limit, but never above 20 MiB.
       A file that is not a genuine PDF, JPG or PNG — whatever its name says — is refused with
       \`attach a PDF, JPG or PNG file whose contents match its extension. Choose the attachment again
       after correcting it\`.`,
    ),

    faq([
      {
        q: 'The invoice has not arrived yet. Should I wait?',
        a: `No. Raise the request, say so in **Purpose** — or in the exception field if your
            organisation requires a document — and attach the invoice when it comes, while the request
            is still awaiting approval.`,
      },
      {
        q: 'My file will not upload.',
        a: `Check the size against the figure the uploader quotes, and that the file really is a PDF,
            JPG or PNG. A scan at full resolution is often the culprit; re-export it smaller.`,
      },
      {
        q: 'Can my approver attach something?',
        a: `No. Documents on a request come only from the person who raised it — on the form, the edit
            form, the correction screen or the hold banner. Anybody else is refused with
            *Only the person who raised a request may add documents to it.* Your approver can ask for
            a document in the conversation, or return the request so you can attach it.`,
      },
      {
        q: 'I attached the file but the request was refused. Was it kept?',
        a: `No. The document and the request are written in one go, so a refused submission leaves
            nothing behind. Attach it again with your corrected form.`,
      },
    ]),
  ],
});
