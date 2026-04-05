import DomainRecordsClient from './client'
export const dynamicParams = false

export function generateStaticParams() {
  return [{ id: '_' }]
}

export default function DomainRecordsPage() {
  return <DomainRecordsClient />
}
