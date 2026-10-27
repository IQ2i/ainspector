<?php

namespace App\Controller;

use App\Entity\Account;
use App\Repository\AccountRepository;
use Symfony\Bridge\Doctrine\Attribute\MapEntity;
use Symfony\Component\HttpFoundation\Response;
use Symfony\Component\Routing\Attribute\Route;
use Symfony\Component\Security\Http\Attribute\IsGranted;

#[Route('/accounts')]
#[IsGranted('ROLE_USER')]
class AccountController extends AbstractController
{
    public function __construct(private AccountRepository $accounts)
    {
    }

    #[Route('/', name: 'account_index')]
    public function index(): Response
    {
        return $this->render('account/index.html.twig', ['accounts' => $this->accounts->findBy(['owner' => $this->getUser()])]);
    }

    #[Route('/{id}', name: 'account_show')]
    public function show(#[MapEntity] Account $account): Response
    {
        return $this->render('account/show.html.twig', ['account' => $account]);
    }
}
