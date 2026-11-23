import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'

import { copy } from '@/copy'
import { Button, Empty, PageHeader, Tag, buttonClasses, useToast } from '@/ui'

import {
  meQuery,
  postOnboarding,
  sellerListingsQuery,
  type MeResponse,
  type OnboardingInput,
} from '@/features/seller/api'
import { notify } from '@/features/seller/components'
import { sellerErrorMessage } from '@/features/seller/errors'
import { PayoutAccountCard } from '@/features/seller/PayoutAccountCard'

export const Route = createFileRoute('/console/onboarding')({
  component: SellerOnboardingPage,
})

const page = copy.pages.sellerOnboarding

export function SellerOnboardingPage() {
  const toast = useToast()
  const queryClient = useQueryClient()
  const { data } = useQuery(meQuery())
  const seller = data?.seller
  const [accepted, setAccepted] = useState(false)

  const signed = seller?.progress.agreement_signed ?? false
  const listings = useQuery({ ...sellerListingsQuery(), enabled: signed })
  const listingCount = listings.data?.length ?? 0

  const mutation = useMutation({
    mutationFn: (input: OnboardingInput) => postOnboarding(input),
    onSuccess: (response) => {
      queryClient.setQueryData<MeResponse>(meQuery().queryKey, (old) =>
        old ? { ...old, seller: response.seller } : old,
      )
      void queryClient.invalidateQueries({ queryKey: meQuery().queryKey })
      notify(toast, { title: copy.pages.sellerCommon.toast.saved, tone: 'success' })
    },
  })

  if (!seller) return null

  const blocked = seller.exists && (seller.status === 'banned' || seller.status === 'closed')
  const errorText = mutation.isError ? sellerErrorMessage(mutation.error) : null
  const allDone = signed && seller.progress.payout_account_bound && listingCount > 0

  function submit(input: OnboardingInput) {
    mutation.reset()
    mutation.mutate(input)
  }

  return (
    <>
      <PageHeader
        title={page.title}
        lead={page.lead}
        actions={
          seller.progress.agreement_signed ? (
            <Tag tone="brand" dot>
              {page.activeBadge}
            </Tag>
          ) : (
            <Tag tone="neutral" dot>
              {page.pendingBadge}
            </Tag>
          )
        }
      />

      {blocked ? (
        <Empty
          className="mt-6"
          title={page.blocked.title}
          reason={page.blocked.reason}
          next={page.blocked.next}
        />
      ) : (
        <>
          {!signed ? (
            <section className="mt-6 glass p-6">
              <header className="flex flex-wrap items-center justify-between gap-2 border-b border-line pb-4">
                <h2 className="text-title font-semibold tracking-tight text-ink">
                  {page.agreement.title}
                </h2>
                <Tag>{page.agreement.version}</Tag>
              </header>

              <div className="space-y-5 pt-5">
                <p className="text-meta text-ink-3">{page.agreement.hint}</p>

                <ol className="divide-y divide-line/70 border-y border-line">
                  {page.agreement.clauses.map((clause) => (
                    <li key={clause.title} className="py-3.5">
                      <h3 className="text-meta font-medium text-ink">{clause.title}</h3>
                      <p className="mt-1 text-meta leading-6 text-ink-3">{clause.body}</p>
                    </li>
                  ))}
                </ol>

                <div className="flex flex-wrap items-center justify-between gap-3">
                  <label className="flex cursor-pointer items-start gap-2 text-meta text-ink">
                    <input
                      type="checkbox"
                      checked={accepted}
                      onChange={(event) => setAccepted(event.target.checked)}
                      className="mt-0.5 h-4 w-4 accent-brand"
                    />
                    <span>{page.agreement.checkbox}</span>
                  </label>
                  <span className="flex items-center gap-4">
                    <Link
                      to="/agreement"
                      className="text-meta text-brand underline-offset-4 hover:underline"
                    >
                      {copy.footer.terms}
                    </Link>
                    <Link
                      to="/privacy"
                      className="text-meta text-brand underline-offset-4 hover:underline"
                    >
                      {copy.footer.privacy}
                    </Link>
                  </span>
                </div>

                {errorText ? (
                  <p className="text-meta text-danger" role="alert">
                    {errorText}
                  </p>
                ) : null}

                <div className="flex justify-end border-t border-line pt-4">
                  <Button
                    disabled={!accepted}
                    loading={mutation.isPending}
                    onClick={() => submit({ agreement_version: 'v1' })}
                  >
                    {mutation.isPending ? page.agreement.submitting : page.agreement.submit}
                  </Button>
                </div>
              </div>
            </section>
          ) : null}

          {signed && !seller.progress.payout_account_bound ? (
            <>
              <div className="mt-6 flex items-start gap-2 text-meta leading-6 text-ink-2">
                <span className="mt-0.5 text-brand" aria-hidden="true">
                  ▸
                </span>
                <p>{page.payoutHint}</p>
              </div>
              <PayoutAccountCard seller={seller} className="mt-3" />
            </>
          ) : null}

          {signed && seller.progress.payout_account_bound && listingCount === 0 ? (
            <section className="mt-6 glass p-6">
              <h2 className="text-title font-semibold tracking-tight text-ink">
                {page.firstListing.title}
              </h2>
              <p className="mt-2 text-body leading-6 text-ink-2">{page.firstListing.reason}</p>
              <div className="mt-4 flex flex-wrap gap-2">
                <Link to="/console/listings/new" className={buttonClasses('primary', 'md')}>
                  {page.firstListing.action}
                </Link>
                <Link to="/console/listings" className={buttonClasses('secondary', 'md')}>
                  {page.firstListing.viewAll}
                </Link>
              </div>
            </section>
          ) : null}

          {allDone ? (
            <section className="mt-6 glass p-6">
              <h2 className="text-title font-semibold tracking-tight text-ink">
                {page.done.title}
              </h2>
              <p className="mt-2 text-body leading-6 text-ink-2">{page.done.reason}</p>
              <div className="mt-4 flex flex-wrap gap-2">
                <Link to="/console/listings/new" className={buttonClasses('primary', 'md')}>
                  {page.done.newListing}
                </Link>
                <Link to="/console/earnings" className={buttonClasses('secondary', 'md')}>
                  {page.done.overview}
                </Link>
              </div>
            </section>
          ) : null}
        </>
      )}
    </>
  )
}
