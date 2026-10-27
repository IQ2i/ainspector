<?php

namespace App\Controller;

use App\Entity\Invoice;
use App\Repository\InvoiceRepository;
use Symfony\Bridge\Doctrine\Attribute\MapEntity;
use Symfony\Component\HttpFoundation\Response;
use Symfony\Component\Routing\Attribute\Route;
use Symfony\Component\Security\Http\Attribute\IsGranted;

#[Route('/invoices')]
#[IsGranted('ROLE_USER')]
class InvoiceController extends AbstractController
{
    public function __construct(private InvoiceRepository $invoices)
    {
    }

    #[Route('/', name: 'invoice_index')]
    public function index(): Response
    {
        return $this->render('invoice/index.html.twig', ['invoices' => $this->invoices->findBy(['owner' => $this->getUser()])]);
    }

    #[Route('/{id}', name: 'invoice_show')]
    #[IsGranted('INVOICE_VIEW', 'invoice')]
    public function show(#[MapEntity] Invoice $invoice): Response
    {
        return $this->render('invoice/show.html.twig', ['invoice' => $invoice]);
    }
}
